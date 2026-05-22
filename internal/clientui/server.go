package clientui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"pspxy/internal/clientconfig"
	"pspxy/internal/clienttunnel"
	clientembed "pspxy/internal/embed/client"
)

// Run 在 listenAddr（如 ":9420"）上托管客户端 SPA + 多代理隧道控制 API。
func Run(listenAddr string, reg *clienttunnel.Registry, clientConfigPath string) error {
	clientConfigPath = filepath.Clean(clientConfigPath)
	tryBootstrapFromYAML(reg, clientConfigPath)

	mux := http.NewServeMux()
	hStart := cors(tunnelStart(reg, clientConfigPath))
	mux.HandleFunc("/api/tunnel/start", hStart)
	mux.HandleFunc("/api/tunnel/proxy/start", hStart)

	mux.HandleFunc("/api/tunnel/stop", cors(tunnelStop(reg, clientConfigPath)))
	mux.HandleFunc("/api/tunnel/proxy/stop", cors(tunnelStop(reg, clientConfigPath)))

	mux.HandleFunc("/api/tunnel/proxy/remove", cors(tunnelRemove(reg, clientConfigPath)))

	mux.HandleFunc("/api/tunnel/status", cors(tunnelAggregateStatus(reg)))

	mux.Handle("/", clientembed.CreateFileServer())

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	log.Printf("客户端 Web UI: http://127.0.0.1%s (仅本机监听时请在 URL 中用 127.0.0.1)", listenAddr)
	go func() {
		time.Sleep(200 * time.Millisecond)
		openBrowser(clientDashboardURL(listenAddr))
	}()

	return server.ListenAndServe()
}

func tryBootstrapFromYAML(reg *clienttunnel.Registry, path string) {
	list, err := clientconfig.LoadProxies(path)
	if errors.Is(err, clientconfig.ErrNoClientConfigFile) {
		return
	}
	if err != nil {
		log.Printf("[client ui] 读取客户端配置文件 %s: %v", path, err)
		return
	}
	reg.BootstrapFromConfigs(list)
	absPath, errAbs := filepath.Abs(path)
	if errAbs != nil {
		log.Printf("[client ui] 已尝试根据 %s 自动恢复隧道", path)
		return
	}
	log.Printf("[client ui] 已尝试根据 %s 自动恢复多条隧道配置", absPath)
}

func clientDashboardURL(listenAddr string) string {
	host := "127.0.0.1"
	port := ""
	if len(listenAddr) > 0 && listenAddr[0] == ':' {
		port = listenAddr[1:]
	} else if listenAddr != "" {
		host, port = splitHostPort(listenAddr)
	}
	if port != "" {
		return fmt.Sprintf("http://%s:%s/", host, port)
	}
	return "http://" + listenAddr + "/"
}

func splitHostPort(addr string) (host, port string) {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[:i], addr[i+1:]
		}
	}
	return addr, ""
}

func cors(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func tunnelStart(reg *clienttunnel.Registry, clientConfigPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		dec := json.NewDecoder(r.Body)
		var cfg clienttunnel.Config
		if err := dec.Decode(&cfg); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := reg.Start(cfg); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		st := reg.Snapshot()
		attachPersistResult(&st, clientConfigPath, reg)
		writeJSON(w, http.StatusOK, st)
	}
}

type stopBody struct {
	ID      string `json:"id"`
	StopAll bool   `json:"stop_all"`
}

func tunnelStop(reg *clienttunnel.Registry, clientConfigPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		raw, _ := io.ReadAll(r.Body)

		if len(strings.TrimSpace(string(raw))) == 0 {
			_ = reg.Stop(clienttunnel.DefaultSingleProxyID)
		} else {
			var req stopBody
			if err := json.Unmarshal(raw, &req); err != nil {
				jsonErr(w, http.StatusBadRequest, "invalid json body")
				return
			}
			if req.StopAll {
				reg.StopAll()
			} else if strings.TrimSpace(req.ID) != "" {
				_ = reg.Stop(strings.TrimSpace(req.ID))
			} else {
				_ = reg.Stop(clienttunnel.DefaultSingleProxyID)
			}
		}

		st := reg.Snapshot()
		attachPersistResult(&st, clientConfigPath, reg)
		writeJSON(w, http.StatusOK, st)
	}
}

type removeBody struct {
	ID string `json:"id"`
}

func tunnelRemove(reg *clienttunnel.Registry, clientConfigPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req removeBody
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		req.ID = strings.TrimSpace(req.ID)
		if req.ID == "" {
			jsonErr(w, http.StatusBadRequest, "缺少 id")
			return
		}
		if err := reg.Remove(req.ID); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		st := reg.Snapshot()
		attachPersistResult(&st, clientConfigPath, reg)
		writeJSON(w, http.StatusOK, st)
	}
}

func tunnelAggregateStatus(reg *clienttunnel.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, reg.Snapshot())
	}
}

func attachPersistResult(st *clienttunnel.MultiStatus, clientConfigPath string, reg *clienttunnel.Registry) {
	absPath, errAbs := filepath.Abs(clientConfigPath)
	if errAbs != nil {
		absPath = clientConfigPath
	}
	st.ClientConfigFile = ""
	st.PersistWarning = ""

	if err := clientconfig.SaveProxies(clientConfigPath, reg.PersistedConfigs()); err != nil {
		st.PersistWarning = fmt.Sprintf("运行态已更新，但写入配置文件 %s 失败: %v", absPath, err)
		log.Printf("[client ui] persist %s: %v", absPath, err)
		return
	}
	st.ClientConfigFile = absPath
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func jsonErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "windows":
		exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start() // #nosec G204
	case "darwin":
		exec.Command("open", url).Start()
	default:
		exec.Command("xdg-open", url).Start()
	}
}
