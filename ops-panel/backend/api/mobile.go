package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/bejix/upstream-ops/backend/operations"
	"github.com/gin-gonic/gin"
)

const (
	hostRoot     = "/hostroot"
	hostProc     = "/host/proc"
	dockerSocket = "/var/run/docker.sock"
)

func registerMobile(g *gin.RouterGroup, d *Deps) {
	mg := g.Group("/mobile")
	mg.GET("/system/overview", func(c *gin.Context) { mobileSystemOverview(c, d) })
	mg.POST("/topup", func(c *gin.Context) { mobileTopup(c, d) })
	mg.GET("/users", func(c *gin.Context) { mobileUsers(c, d) })
	mg.POST("/docker/restart", func(c *gin.Context) { dockerRestart(c) })
	mg.GET("/docker/images", func(c *gin.Context) { dockerImages(c) })
	mg.POST("/docker/rollback", func(c *gin.Context) { dockerRollback(c) })
}

// ---------------- system overview ----------------

type containerInfo struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	State  string            `json:"state"`
	Status string            `json:"status"`
	Image  string            `json:"image"`
	Labels map[string]string `json:"labels,omitempty"`
}

func mobileSystemOverview(c *gin.Context, d *Deps) {
	overview := gin.H{
		"host":   hostMetrics(),
		"docker": dockerContainers(),
	}
	c.JSON(http.StatusOK, gin.H{"data": overview})
}

func hostMetrics() gin.H {
	cpu := readHostCPUUsage()
	mem := readHostMem()
	load, loadErr := readFirstLine(filepath.Join(hostProc, "loadavg"))
	uptime, uptimeErr := readFirstLine(filepath.Join(hostProc, "uptime"))

	disk := gin.H{"available": false}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(hostRoot, &stat); err == nil {
		bsize := uint64(stat.Bsize)
		total := stat.Blocks * bsize
		free := stat.Bavail * bsize
		used := (stat.Blocks - stat.Bavail) * bsize
		disk = gin.H{
			"available": true,
			"total":     total,
			"free":      free,
			"used":      used,
		}
		if total > 0 {
			disk["used_percent"] = float64(used) / float64(total) * 100
		}
	}

	return gin.H{
		"cpu":         cpu,
		"memory":      mem,
		"loadavg":     load,
		"loadavg_err": loadErr,
		"uptime":      uptime,
		"uptime_err":  uptimeErr,
		"disk":        disk,
	}
}

// readHostCPUUsage 两次采样 /host/proc/stat 计算宿主 CPU 使用率。
func readHostCPUUsage() gin.H {
	sample := func() (idle, total uint64, ok bool) {
		file, err := os.Open(filepath.Join(hostProc, "stat"))
		if err != nil {
			return 0, 0, false
		}
		defer file.Close()
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			fields := strings.Fields(scanner.Text())
			if len(fields) < 5 || fields[0] != "cpu" {
				continue
			}
			var values [8]uint64
			for i := 1; i < len(fields) && i <= 8; i++ {
				values[i-1], _ = strconv.ParseUint(fields[i], 10, 64)
			}
			idle = values[3] + values[4]
			for _, v := range values {
				total += v
			}
			return idle, total, true
		}
		return 0, 0, false
	}

	firstIdle, firstTotal, ok := sample()
	if !ok {
		return gin.H{"available": false}
	}
	time.Sleep(200 * time.Millisecond)
	secondIdle, secondTotal, ok := sample()
	if !ok {
		return gin.H{"available": false}
	}

	totalDelta := secondTotal - firstTotal
	if totalDelta == 0 {
		return gin.H{"available": false}
	}
	idleDelta := secondIdle - firstIdle
	usedPercent := float64(totalDelta-idleDelta) / float64(totalDelta) * 100
	return gin.H{"available": true, "used_percent": usedPercent}
}

func readHostMem() gin.H {
	file, err := os.Open(filepath.Join(hostProc, "meminfo"))
	if err != nil {
		return gin.H{"available": false}
	}
	defer file.Close()

	result := gin.H{"available": false}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		key := strings.TrimSuffix(fields[0], ":")
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			continue
		}
		value *= 1024
		switch key {
		case "MemTotal":
			result["total"] = value
		case "MemAvailable":
			result["available"] = true
			result["free"] = value
		}
	}
	if total, ok := result["total"].(uint64); ok && total > 0 {
		free, _ := result["free"].(uint64)
		result["used"] = total - free
		result["used_percent"] = float64(total-free) / float64(total) * 100
	}
	return result
}

func readFirstLine(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	if scanner.Scan() {
		return scanner.Text(), nil
	}
	return "", scanner.Err()
}

// dockerClient 构造走 unix socket 的 HTTP 客户端。
func dockerClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.DialTimeout("unix", dockerSocket, 3*time.Second)
			},
		},
		Timeout: timeout,
	}
}

// dockerRequest 向 docker daemon 发起请求。
func dockerRequest(method, path string, body []byte, timeout time.Duration) (*http.Response, error) {
	client := dockerClient(timeout)
	var req *http.Request
	var err error
	if body != nil {
		req, err = http.NewRequest(method, "http://docker"+path, bytes.NewReader(body))
	} else {
		req, err = http.NewRequest(method, "http://docker"+path, nil)
	}
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return client.Do(req)
}

// dockerContainers 通过挂载的 docker.sock 查询容器列表。
func dockerContainers() []containerInfo {
	resp, err := dockerRequest(http.MethodGet, "/containers/json?all=1", nil, 5*time.Second)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var raw []struct {
		ID     string            `json:"Id"`
		Names  []string          `json:"Names"`
		State  string            `json:"State"`
		Status string            `json:"Status"`
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil
	}

	containers := make([]containerInfo, 0, len(raw))
	for _, item := range raw {
		name := ""
		if len(item.Names) > 0 {
			name = strings.TrimPrefix(item.Names[0], "/")
		}
		shortID := item.ID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}
		containers = append(containers, containerInfo{
			ID:     shortID,
			Name:   name,
			State:  item.State,
			Status: item.Status,
			Image:  item.Image,
			Labels: item.Labels,
		})
	}
	return containers
}

// ---------------- docker 操作 ----------------

// composeOverlay 是部署时叠加的 compose 文件，回滚只允许改这个文件里的 image 行。
const composeOverlay = "/opt/sub2api/docker-compose.codex-current.yml"

// hostRootPath 是宿主根文件系统在容器内的挂载点（bind ro）。
const hostRootPath = "/hostroot/opt/sub2api"

// composeProjectDir 是容器内的项目目录（各 compose 与 .env 单文件挂载在此）。
const composeProjectDir = "/opt/sub2api"

// composeFiles 是 ops 部署实际使用的 compose 文件列表（与部署脚本一致），
// 相对 /opt/sub2api 的路径；rollback 时复制到容器内可写目录再执行 compose。
var composeFiles = []string{
	"docker-compose.yml",
	"docker-compose.runtime.yml",
	"docker-compose.ops.yml",
	"ops-panel-s2a/operations/docker-compose.ops.yml",
	"docker-compose.codex-current.yml",
}

// rollbackEnvFiles 是 compose 解析依赖的 env 文件（相对 /opt/sub2api）。
var rollbackEnvFiles = []string{".env", ".ops-panel.env"}

func dockerRestart(c *gin.Context) {
	var in struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" {
		fail(c, http.StatusBadRequest, fmt.Errorf("name is required"))
		return
	}
	resp, err := dockerRequest(http.MethodPost, "/containers/"+url.PathEscape(in.Name)+"/restart?t=10", nil, 90*time.Second)
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		fail(c, http.StatusBadGateway, fmt.Errorf("docker: %s", strings.TrimSpace(string(body))))
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"name": in.Name, "action": "restarted"}})
}

func dockerImages(c *gin.Context) {
	resp, err := dockerRequest(http.MethodGet, "/images/json?all=1", nil, 10*time.Second)
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	defer resp.Body.Close()
	var raw []struct {
		RepoTags []string `json:"RepoTags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	repo := c.Query("repo")
	var tags []string
	for _, img := range raw {
		for _, t := range img.RepoTags {
			if t == "<none>:<none>" {
				continue
			}
			if repo == "" || strings.HasPrefix(t, repo+":") || t == repo {
				tags = append(tags, t)
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"repo": repo, "tags": tags}})
}

// setServiceImage 把 compose overlay 里指定服务的 image 行替换为 target。
func setServiceImage(service, target string) error {
	content, err := os.ReadFile(composeOverlay)
	if err != nil {
		return err
	}
	lines := strings.Split(string(content), "\n")
	inService := false
	changed := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "    ") && strings.HasSuffix(line, ":") {
			name := strings.TrimSpace(strings.TrimSuffix(line, ":"))
			inService = name == service
			continue
		}
		if inService && strings.HasPrefix(trimmed, "image:") {
			indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
			lines[i] = indent + "image: " + target
			changed = true
			break
		}
	}
	if !changed {
		return fmt.Errorf("service %q not found in %s", service, composeOverlay)
	}
	return os.WriteFile(composeOverlay, []byte(strings.Join(lines, "\n")), 0o644)
}

func dockerRollback(c *gin.Context) {
	var in struct {
		Name  string `json:"name"`
		Image string `json:"image"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || in.Name == "" || in.Image == "" {
		fail(c, http.StatusBadRequest, fmt.Errorf("name and image are required"))
		return
	}

	// 1. 容器必须属于 sub2api compose 项目，且 overlay 里能定位到服务
	resp, err := dockerRequest(http.MethodGet, "/containers/"+url.PathEscape(in.Name)+"/json", nil, 10*time.Second)
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	var detail struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
	}
	decodeErr := json.NewDecoder(resp.Body).Decode(&detail)
	resp.Body.Close()
	if decodeErr != nil {
		fail(c, http.StatusBadGateway, decodeErr)
		return
	}
	project := detail.Config.Labels["com.docker.compose.project"]
	service := detail.Config.Labels["com.docker.compose.service"]
	if project != "sub2api" || service == "" {
		fail(c, http.StatusBadRequest, fmt.Errorf("container %q is not managed by sub2api compose project", in.Name))
		return
	}

	// 2. 目标镜像必须存在
	imgResp, err := dockerRequest(http.MethodGet, "/images/json?all=1", nil, 10*time.Second)
	if err != nil {
		fail(c, http.StatusBadGateway, err)
		return
	}
	var imgs []struct {
		RepoTags []string `json:"RepoTags"`
	}
	imgErr := json.NewDecoder(imgResp.Body).Decode(&imgs)
	imgResp.Body.Close()
	if imgErr != nil {
		fail(c, http.StatusBadGateway, imgErr)
		return
	}
	found := false
	for _, img := range imgs {
		for _, t := range img.RepoTags {
			if t == in.Image {
				found = true
			}
		}
	}
	if !found {
		fail(c, http.StatusBadRequest, fmt.Errorf("image %q not found locally", in.Image))
		return
	}

	// 3. 备份 overlay
	stamp := time.Now().UTC().Format("20060102T150405Z")
	backupDir := filepath.Join("/app/data", "auto-rollback-"+stamp)
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	backupPath := filepath.Join(backupDir, "docker-compose.codex-current.yml")
	if data, err := os.ReadFile(composeOverlay); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	} else if err := os.WriteFile(backupPath, data, 0o644); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}

	// 4. 替换 image 行
	if err := setServiceImage(service, in.Image); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}

	// 5. 组装可写项目目录（容器内无 /opt/sub2api 目录，compose 客户端需要本地文件），
	//    从宿主挂载 /hostroot 复制 compose 与 env 文件后 compose up 重建该服务
	rollbackDir := filepath.Join("/app/data", "rollback-project-"+stamp)
	if err := os.MkdirAll(rollbackDir, 0o755); err != nil {
		fail(c, http.StatusInternalServerError, err)
		return
	}
	copyIn := func(rel string) error {
		// 优先从容器内单文件挂载的路径读（ops-panel-s2a/operations 是符号链接，/hostroot 视角无法跟随），
		// 读不到再从宿主根挂载 /hostroot 读。
		var data []byte
		var err error
		data, err = os.ReadFile(filepath.Join(composeProjectDir, rel))
		if err != nil {
			data, err = os.ReadFile(filepath.Join(hostRootPath, rel))
		}
		if err != nil {
			return err
		}
		dst := filepath.Join(rollbackDir, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	}
	for _, rel := range append(append([]string{}, composeFiles...), rollbackEnvFiles...) {
		if err := copyIn(rel); err != nil {
			fail(c, http.StatusInternalServerError, fmt.Errorf("copy %s: %w", rel, err))
			return
		}
	}
	args := []string{"compose", "--project-name", "sub2api"}
	for _, f := range composeFiles {
		args = append(args, "-f", f)
	}
	args = append(args, "up", "-d", "--no-deps", "--force-recreate", service)
	cmd := exec.Command("docker", args...)
	cmd.Dir = rollbackDir
	output, err := cmd.CombinedOutput()
	if err != nil {
		// 重建失败：还原 overlay
		_ = os.WriteFile(composeOverlay, func() []byte {
			data, _ := os.ReadFile(backupPath)
			return data
		}(), 0o644)
		fail(c, http.StatusBadGateway, fmt.Errorf("compose up failed: %s", strings.TrimSpace(string(output))))
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"name":    in.Name,
		"service": service,
		"image":   in.Image,
		"action":  "rolled_back",
		"output":  strings.TrimSpace(string(output)),
	}})
}
// ---------------- topup ----------------

func mobileUsers(c *gin.Context, d *Deps) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	users, err := d.Operations.FindUsers(ctx, c.Query("q"), 20)
	if err != nil {
		status := operations.ErrorStatus(err)
		fail(c, status, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": gin.H{"users": users}})
}

func mobileTopup(c *gin.Context, d *Deps) {
	var in struct {
		UserID     int64   `json:"user_id"`
		Identifier string  `json:"identifier"`
		Amount     float64 `json:"amount"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		fail(c, http.StatusBadRequest, err)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	actor := ""
	if username := c.GetString("username"); username != "" {
		actor = username
	} else {
		actor = "mobile"
	}

	var result *operations.TopupResult
	var err error
	if in.UserID > 0 {
		result, err = d.Operations.Topup(ctx, in.UserID, in.Amount, actor)
	} else {
		result, err = d.Operations.TopupByIdentifier(ctx, in.Identifier, in.Amount, actor)
	}
	if err != nil {
		status := operations.ErrorStatus(err)
		fail(c, status, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": result})
}