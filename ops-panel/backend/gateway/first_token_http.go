// 首字超时 HTTP 执行与响应首块读取。
package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

func (svc *Service) remainingFirstTokenWait(start time.Time, configured time.Duration) (left time.Duration, timedOut bool) {
	if configured <= 0 {
		return 0, false
	}
	left = configured - time.Since(start)
	if left <= 0 {
		return 0, true
	}
	return left, false
}

// doHTTPWithFirstTokenDeadline 执行 client.Do；首字超时从 start 起算。
// 等响应头超过预算时 cancel 请求并返回 errFirstTokenTimeout（与 body 首字节同一预算）。
// abort 在超时路径调用以打断卡住的连接；成功拿到响应后不得 abort（否则 body 被取消）。
func (svc *Service) doHTTPWithFirstTokenDeadline(
	parent context.Context,
	abort context.CancelFunc,
	client *http.Client,
	req *http.Request,
	start time.Time,
	firstTokenTimeout time.Duration,
) (*http.Response, error) {
	if client == nil {
		return nil, errors.New("http client is nil")
	}
	if firstTokenTimeout <= 0 {
		return client.Do(req)
	}
	left, timedOut := svc.remainingFirstTokenWait(start, firstTokenTimeout)
	if timedOut {
		if abort != nil {
			abort()
		}
		return nil, fmt.Errorf("%w after %s", errFirstTokenTimeout, firstTokenTimeout)
	}

	type doResult struct {
		resp *http.Response
		err  error
	}
	ch := make(chan doResult, 1)
	go func() {
		resp, err := client.Do(req)
		ch <- doResult{resp: resp, err: err}
	}()

	timer := time.NewTimer(left)
	defer timer.Stop()

	select {
	case r := <-ch:
		if r.err != nil {
			// 若 parent 已取消且接近首字截止，归为首字超时更易理解
			if parent != nil && parent.Err() != nil && svc.isFirstTokenTimeout(r.err) {
				return nil, r.err
			}
			if r.err != nil && (errors.Is(r.err, context.Canceled) || errors.Is(r.err, context.DeadlineExceeded)) {
				if _, to := svc.remainingFirstTokenWait(start, firstTokenTimeout); to {
					return nil, fmt.Errorf("%w after %s", errFirstTokenTimeout, firstTokenTimeout)
				}
			}
			return nil, r.err
		}
		return r.resp, nil
	case <-timer.C:
		if abort != nil {
			abort()
		}
		// 等待 Do 退出，避免泄漏；若已返回响应则关掉 body
		r := <-ch
		if r.resp != nil {
			_ = r.resp.Body.Close()
		}
		return nil, fmt.Errorf("%w after %s", errFirstTokenTimeout, firstTokenTimeout)
	case <-parent.Done():
		if abort != nil {
			abort()
		}
		r := <-ch
		if r.resp != nil {
			_ = r.resp.Body.Close()
		}
		// parent 取消：若首字预算也耗尽，优先报首字超时
		if _, to := svc.remainingFirstTokenWait(start, firstTokenTimeout); to {
			return nil, fmt.Errorf("%w after %s", errFirstTokenTimeout, firstTokenTimeout)
		}
		if parent.Err() != nil {
			return nil, parent.Err()
		}
		return nil, context.Canceled
	}
}

// readFirstChunk 读取响应体首块；timeout<=0 时无超时限制。
// timeout>0 时为「剩余」等待时间（调用方应已扣掉等响应头耗时）。
func (svc *Service) readFirstChunk(r io.Reader, timeout time.Duration) ([]byte, error) {
	if timeout <= 0 {
		// timeout==0：可能是「关闭首字超时」或「剩余时间用尽」。
		// 用尽应由 remainingFirstTokenWait 在调用前拦截；此处 0 按无限制读。
		buf := make([]byte, 32*1024)
		n, err := r.Read(buf)
		if n > 0 {
			return buf[:n], nil
		}
		if err == io.EOF {
			return nil, nil
		}
		return nil, err
	}
	type readResult struct {
		n   int
		err error
		buf []byte
	}
	ch := make(chan readResult, 1)
	go func() {
		buf := make([]byte, 32*1024)
		n, err := r.Read(buf)
		out := readResult{n: n, err: err}
		if n > 0 {
			out.buf = append([]byte(nil), buf[:n]...)
		}
		ch <- out
	}()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case res := <-ch:
		if res.n > 0 {
			return res.buf, nil
		}
		if res.err == io.EOF {
			return nil, nil
		}
		return nil, res.err
	case <-timer.C:
		return nil, fmt.Errorf("%w after %s", errFirstTokenTimeout, timeout)
	}
}

func (svc *Service) copyResponseHeaders(dst, src http.Header) {
	// hop-by-hop 与网关自有 request id 不从上游抄写；后者由 setGatewayRequestIDHeaders 单独写入
	skip := map[string]struct{}{
		"Content-Length": {}, "Content-Encoding": {}, "Transfer-Encoding": {},
		"Connection": {},
		http.CanonicalHeaderKey(headerUpstreamOpsRequestID): {},
	}
	for k, vals := range src {
		if _, ok := skip[http.CanonicalHeaderKey(k)]; ok {
			continue
		}
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}
