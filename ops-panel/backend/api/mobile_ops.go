package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

// registerMobileOps 挂载「手机远程运维」增强接口：
// 容器日志、启停；受控远程命令（白名单 + 超时 + 输出截断）。
// 鉴权复用 /api 组的 AuthMiddleware。
func registerMobileOps(g *gin.RouterGroup, d *Deps) {
	mg := g.Group("/mobile")
	mg.GET("/container/logs", func(c *gin.Context) { mobileContainerLogs(c) })
	mg.POST("/container/start", func(c *gin.Context) { mobileContainerAction(c, "start") })
	mg.POST("/container/stop", func(c *gin.Context) { mobileContainerAction(c, "stop") })
	mg.POST("/ssh/exec", func(c *gin.Context) { mobileSSHExec(c) })
}

// ---------------- 容器日志 ----------------

func mobileContainerLogs(c *gin.Context) {
	name := strings.TrimSpace(c.Query("name"))
	tail := strings.TrimSpace(c.DefaultQuery("tail", "200"))
	if name == "" {
		fail(c, http.StatusBadRequest, fmt.Errorf("name is required"))
		return
	}
	if !isDigits(tail) {
		tail = "200"
	}
	resp, err := dockerRequest(http.MethodGet, "/containers/"+url.PathEscape(name)+"/logs?stdout=1&stderr=1&tail="+tail+"&timestamps=1", nil, 15*time.Second)
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		fail(c, http.StatusBadGateway, fmt.Errorf("docker: %s", strings.TrimSpace(string(body))))
		return
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"name": name, "logs": stripDockerStreamFrames(raw)}})
}

// stripDockerStreamFrames 去掉 docker logs 的 multiplexed stream 帧头（8 字节）。
func stripDockerStreamFrames(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	// 非流式响应直接是纯文本：开头是可打印 ASCII 或换行时按原文返回。
	if raw[0] >= 0x20 || raw[0] == '\n' || raw[0] == '\t' {
		return strings.TrimRight(string(raw), "\n")
	}
	var sb strings.Builder
	for i := 0; i+8 <= len(raw); {
		header := raw[i : i+8]
		payloadLen := int(header[4])<<24 | int(header[5])<<16 | int(header[6])<<8 | int(header[7])
		if payloadLen < 0 || i+8+payloadLen > len(raw) {
			break
		}
		sb.Write(raw[i+8 : i+8+payloadLen])
		i += 8 + payloadLen
	}
	if sb.Len() == 0 {
		return strings.TrimRight(string(raw), "\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// ---------------- 容器启停 ----------------

func mobileContainerAction(c *gin.Context, action string) {
	var in struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" {
		fail(c, http.StatusBadRequest, fmt.Errorf("name is required"))
		return
	}
	var dockerPath string
	switch action {
	case "start":
		dockerPath = "/containers/" + url.PathEscape(in.Name) + "/start"
	case "stop":
		dockerPath = "/containers/" + url.PathEscape(in.Name) + "/stop?t=10"
	default:
		fail(c, http.StatusBadRequest, fmt.Errorf("unsupported action"))
		return
	}
	resp, err := dockerRequest(http.MethodPost, dockerPath, nil, 90*time.Second)
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	defer resp.Body.Close()
	// 204 = 成功；304 = 已处于目标状态，同样视为成功。
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotModified {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		fail(c, http.StatusBadGateway, fmt.Errorf("docker: %s", strings.TrimSpace(string(body))))
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"name": in.Name, "action": action, "status": resp.StatusCode}})
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// ---------------- 受控远程命令 ----------------

// sshCommandWhitelist 是手机端可执行的命令前缀白名单。
// 注意：条目不能包含 ; | & 等禁用字符；前缀后必须是参数边界（空格或结尾）。
var sshCommandWhitelist = []string{
	"uptime",
	"df -h",
	"free -h",
	"docker ps",
	"docker stats",
	"docker images",
	"docker logs",
	"docker inspect",
	"docker compose ps",
	"docker compose logs",
	"docker compose restart",
	"docker compose config",
	"docker-compose ps",
	"systemctl status",
	"journalctl -u",
	"ps aux",
	"top -b",
	"tail -n",
	"cat /opt/sub2api",
	"ls /opt/sub2api",
	"netstat -tulnp",
	"ss -tulnp",
	"ping -c",
	"curl -sS",
	"nvidia-smi",
	"du -sh",
	"redis-cli ping",
}

// sshForbidSubstrings 是命令中禁止出现的片段（无 shell 解释，纯防御纵深）。
var sshForbidSubstrings = []string{";", "|", "&", "`", "$(", "${", ">>", ">", "<", "\n", "\r"}

// whitelisted 判断 cmdLine 是否命中白名单前缀（前缀后须为空格/结尾/路径分隔符）。
func whitelisted(cmdLine string) bool {
	for _, prefix := range sshCommandWhitelist {
		if strings.HasPrefix(cmdLine, prefix) {
			rest := cmdLine[len(prefix):]
			if rest == "" || rest[0] == ' ' || rest[0] == '/' {
				return true
			}
		}
	}
	return false
}

const mobileExecTimeout = 45 * time.Second
const mobileOutputLimit = 128 * 1024

// mobileSSHExec 在宿主上执行白名单内的诊断/运维命令。
// 执行路径三层：
//  1. docker / docker-compose 子命令 → 容器内 docker CLI（socket 已挂载，rollback 同路径）；
//     compose 子命令先组装可写项目目录（复用 rollback 的文件拷贝逻辑）。
//  2. 主机指标类（df/free/uptime）→ 直接读 /host/proc 与 Statfs，零依赖必成功。
//  3. 其余系统命令 → docker run --rm --pid=host chroot 到宿主根执行（best-effort）。
func mobileSSHExec(c *gin.Context) {
	var in struct {
		Command string `json:"command"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || strings.TrimSpace(in.Command) == "" {
		fail(c, http.StatusBadRequest, fmt.Errorf("command is required"))
		return
	}
	cmdLine := strings.TrimSpace(in.Command)

	if !whitelisted(cmdLine) {
		fail(c, http.StatusBadRequest, fmt.Errorf("command not allowed: %s", cmdLine))
		return
	}
	for _, s := range sshForbidSubstrings {
		if strings.Contains(cmdLine, s) {
			fail(c, http.StatusBadRequest, fmt.Errorf("forbidden character in command: %q", s))
			return
		}
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), mobileExecTimeout)
	defer cancel()

	// 主机指标内置实现（不依赖 exec，稳定可用）。
	if out, ok := builtinHostCommand(ctx, cmdLine); ok {
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"command": cmdLine, "output": out, "mode": "builtin"}})
		return
	}

	fields := strings.Fields(cmdLine)

	// docker CLI 路径。
	if fields[0] == "docker" || fields[0] == "docker-compose" {
		out, mode, err := execDockerCommand(ctx, fields)
		if err != nil {
			fail(c, http.StatusBadGateway, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"command": cmdLine, "output": out, "mode": mode}})
		return
	}

	// 系统命令：chroot 到宿主根执行（需要能拉到 alpine:3 镜像）。
	out, err := execInHostChroot(ctx, fields)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"data": gin.H{"command": cmdLine, "output": out, "error": err.Error(), "mode": "chroot"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"command": cmdLine, "output": out, "mode": "chroot"}})
}

// builtinHostCommand 用直读 /host/proc 与 Statfs 实现主机指标命令。
func builtinHostCommand(ctx context.Context, cmdLine string) (string, bool) {
	switch {
	case cmdLine == "uptime":
		load, loadErr := readFirstLine(filepath.Join(hostProc, "loadavg"))
		if loadErr != nil {
			return "", false
		}
		parts := strings.Fields(load)
		uptime, _ := readFirstLine(filepath.Join(hostProc, "uptime"))
		up := ""
		if fields := strings.Fields(uptime); len(fields) > 0 {
			if secs, err := strconv.ParseFloat(strings.TrimSuffix(fields[0], "."), 64); err == nil {
				days := int(secs) / 86400
				hours := (int(secs) % 86400) / 3600
				mins := (int(secs) % 3600) / 60
				up = fmt.Sprintf(" up %d days %d:%02d,", days, hours, mins)
			}
		}
		load1, load5, load15 := "", "", ""
		if len(parts) >= 3 {
			load1, load5, load15 = parts[0], parts[1], parts[2]
		}
		return fmt.Sprintf(" %s load average: %s, %s, %s", up, load1, load5, load15), true

	case cmdLine == "free -h" || cmdLine == "free":
		mem := readHostMem()
		if v, ok := mem["total"].(uint64); ok {
			used, _ := mem["used"].(uint64)
			free, _ := mem["free"].(uint64)
			return fmt.Sprintf("              total        used        free\nMem:  %12s %12s %12s",
				humanBytes(v), humanBytes(used), humanBytes(free)), true
		}
		return "", false

	case cmdLine == "df -h" || cmdLine == "df":
		var stat syscall.Statfs_t
		if err := syscall.Statfs(hostRoot, &stat); err != nil {
			return "", false
		}
		bsize := uint64(stat.Bsize)
		total := stat.Blocks * bsize
		avail := stat.Bavail * bsize
		used := (stat.Blocks - stat.Bavail) * bsize
		pct := float64(used) / float64(total) * 100
		return fmt.Sprintf("Filesystem      Size  Used Avail Use%%\n/hostroot  %8s %6s %6s  %2.0f%%",
			humanBytes(total), humanBytes(used), humanBytes(avail), pct), true
	}
	return "", false
}

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := uint64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(n)/float64(div), "KMGTPE"[exp])
}

// execDockerCommand 执行 docker / docker-compose 命令。
// compose 子命令在组装好的可写项目目录里跑（与 rollback 同机制）。
func execDockerCommand(ctx context.Context, fields []string) (string, string, error) {
	isCompose := false
	if fields[0] == "docker" && len(fields) > 1 && fields[1] == "compose" {
		isCompose = true
	}
	if fields[0] == "docker-compose" {
		isCompose = true
	}

	workDir := "/opt/sub2api"
	mode := "docker-cli"
	if isCompose {
		dir, err := prepareComposeProjectDir()
		if err != nil {
			return "", mode, fmt.Errorf("prepare compose dir: %w", err)
		}
		workDir = dir
		mode = "docker-compose"

		// docker-compose（v1 语法）→ docker compose（v2）。
		if fields[0] == "docker-compose" {
			fields = append([]string{"docker", "compose"}, fields[1:]...)
		}
	}

	cmd := exec.CommandContext(ctx, fields[0], fields[1:]...)
	cmd.Dir = workDir
	out, err := cmd.CombinedOutput()
	output := truncateOutput(out, mobileOutputLimit)
	if err != nil {
		return output + "\n[error] " + err.Error(), mode, nil
	}
	return output, mode, nil
}

// prepareComposeProjectDir 把 compose 与 env 文件拷到 /app/data 下的可写目录。
// 与 dockerRollback 的组装逻辑一致，供 compose 子命令使用。
func prepareComposeProjectDir() (string, error) {
	stamp := time.Now().UTC().Format("20060102T150405Z")
	dir := filepath.Join("/app/data", "mobile-exec-project-"+stamp)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	copyIn := func(rel string) error {
		data, err := os.ReadFile(filepath.Join(composeProjectDir, rel))
		if err != nil {
			data, err = os.ReadFile(filepath.Join(hostRootPath, rel))
		}
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	}
	for _, rel := range append(append([]string{}, composeFiles...), rollbackEnvFiles...) {
		if err := copyIn(rel); err != nil {
			return "", fmt.Errorf("copy %s: %w", rel, err)
		}
	}
	return dir, nil
}

// execInHostChroot 用 docker run --pid=host + chroot 到宿主根执行系统命令。
// alpine:3 仅作 chroot 载体，实际执行的是宿主文件系统里的二进制。
func execInHostChroot(ctx context.Context, fields []string) (string, error) {
	args := []string{"run", "--rm", "--pid=host",
		"-v", "/:/hostroot-exec",
		"-v", "/proc:/hostroot-exec/proc:ro",
		"-v", "/sys:/hostroot-exec/sys:ro",
		"-v", "/dev:/hostroot-exec/dev:ro",
		"alpine:3", "chroot", "/hostroot-exec"}
	args = append(args, fields...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	return truncateOutput(out, mobileOutputLimit), err
}

func truncateOutput(out []byte, limit int) string {
	s := strings.TrimSpace(string(out))
	if len(s) > limit {
		return s[:limit] + "\n... (truncated)"
	}
	return s
}
