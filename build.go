//go:build ignore

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type Target struct {
	Name   string
	GOOS   string
	GOARCH string
	Ext    string
}

var clientTargets = []Target{
	{Name: "tcp-proxy-client-windows-amd64", GOOS: "windows", GOARCH: "amd64", Ext: ".exe"},
	{Name: "tcp-proxy-client-linux-amd64", GOOS: "linux", GOARCH: "amd64", Ext: ""},
}

const (
	buildDir       = "build"
	serverName     = "tcp-proxy-server"
	npmRegistry    = "https://registry.npmmirror.com"
	frontendFolder = "frontend"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	switch cmd {
	case "web-admin":
		buildWebAdmin()
	case "web-client":
		buildWebClient()
	case "web":
		buildWebAdmin()
		buildWebClient()
	case "server":
		buildServer()
	case "clients":
		buildClients()
	case "all":
		buildWebAdmin()
		buildWebClient()
		buildServer()
		buildClients()
	case "clean":
		clean()
	case "run":
		buildWebAdmin()
		buildServer()
		runServer()
	default:
		fmt.Printf("未知命令: %s\n\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("用法: go run build.go <命令>")
	fmt.Println()
	fmt.Println("命令:")
	fmt.Println("  web-admin   构建管理端 React (→ internal/embed/admin/dist)")
	fmt.Println("  web-client  构建客户端 React (→ internal/embed/client/dist)")
	fmt.Println("  web         同时构建 web-admin 与 web-client")
	fmt.Println("  server      先构建 admin 前端，再构建服务端二进制")
	fmt.Println("  clients     先构建 client 前端，再构建各平台客户端")
	fmt.Println("  all         构建全部前端 + server + clients")
	fmt.Println("  clean       清理构建产物")
	fmt.Println("  run         构建 admin + server 并运行服务端")
	fmt.Println()
	fmt.Println("环境变量:")
	fmt.Println("  GOPROXY   默认 https://goproxy.cn,direct")
	fmt.Println()
	fmt.Println("说明: 源码位于 frontend/web-admin / frontend/web-client（mono repo 目录组织）。")
}

func frontendApp(name string) string {
	return filepath.Join(frontendFolder, name)
}

func buildWebAdmin() {
	fmt.Println("=== 构建 web-admin ===")
	dir := frontendApp("web-admin")
	npmBuild(dir)
	dest := filepath.Join("internal", "embed", "admin", "dist")
	os.MkdirAll(dest, 0755)
	copyDir(filepath.Join(dir, "dist"), dest)
	fmt.Println("web-admin 已同步到", dest)
}

func buildWebClient() {
	fmt.Println("=== 构建 web-client ===")
	dir := frontendApp("web-client")
	npmBuild(dir)
	dest := filepath.Join("internal", "embed", "client", "dist")
	os.MkdirAll(dest, 0755)
	copyDir(filepath.Join(dir, "dist"), dest)
	fmt.Println("web-client 已同步到", dest)
}

func npmBuild(dir string) {
	cleanDir := filepath.Clean(dir)
	if strings.Contains(cleanDir, "..") {
		fmt.Fprintf(os.Stderr, "非法目录: %s\n", dir)
		os.Exit(1)
	}
	if !fileExists(filepath.Join(cleanDir, "package.json")) {
		fmt.Fprintf(os.Stderr, "未找到 %s/package.json\n", cleanDir)
		os.Exit(1)
	}
	if !fileExists(filepath.Join(cleanDir, "node_modules")) {
		runCmdDir(cleanDir, "npm", "config", "set", "registry", npmRegistry)
		runCmdDir(cleanDir, "npm", "install")
	}
	runCmdDir(cleanDir, "npm", "run", "build")
}

// buildServer 构建服务端
func buildServer() {
	buildWebAdmin()

	fmt.Println("=== 构建服务端 ===")

	os.MkdirAll(buildDir, 0755)
	ldflags := getLDFlags()
	out := filepath.Join(buildDir, serverName+exeExt())

	env := os.Environ()
	env = append(env, "CGO_ENABLED=0")
	if os.Getenv("GOPROXY") == "" {
		env = append(env, "GOPROXY=https://goproxy.cn,direct")
	}

	runCmdWithEnv(env, "go", "build", "-ldflags", ldflags, "-o", out, "./cmd/server")
	fmt.Printf("服务端构建完成: %s\n", out)
}

// buildClients 构建多平台客户端
func buildClients() {
	buildWebClient()

	fmt.Println("=== 构建客户端 ===")

	os.MkdirAll(buildDir, 0755)
	ldflags := getLDFlags()

	for _, t := range clientTargets {
		out := filepath.Join(buildDir, t.Name+t.Ext)

		env := os.Environ()
		env = append(env, "GOOS="+t.GOOS, "GOARCH="+t.GOARCH, "CGO_ENABLED=0")
		if os.Getenv("GOPROXY") == "" {
			env = append(env, "GOPROXY=https://goproxy.cn,direct")
		}

		fmt.Printf("构建 %s/%s → %s\n", t.GOOS, t.GOARCH, out)
		runCmdWithEnv(env, "go", "build", "-ldflags", ldflags, "-o", out, "./cmd/client")
	}

	fmt.Println("客户端构建完成")
}

func clean() {
	fmt.Println("=== 清理 ===")
	os.RemoveAll(buildDir)
	os.RemoveAll(filepath.Join(frontendApp("web-admin"), "dist"))
	os.RemoveAll(filepath.Join(frontendApp("web-client"), "dist"))
	fmt.Println("清理完成（嵌入目录 internal/embed/*/dist 未删除，可自行同步构建）")
}

func runServer() {
	out := filepath.Join(buildDir, serverName+exeExt())
	fmt.Printf("启动服务端: %s\n", out)
	runCmd(out)
}

func getLDFlags() string {
	version := "dev"
	out, err := exec.Command("git", "describe", "--tags", "--always", "--dirty").Output()
	if err == nil {
		version = strings.TrimSpace(string(out))
	}
	return fmt.Sprintf("-s -w -X main.Version=%s", version)
}

func exeExt() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func runCmd(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "命令失败: %s %v: %v\n", name, args, err)
		os.Exit(1)
	}
}

func runCmdDir(dir string, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "命令失败 [%s] %s %v: %v\n", dir, name, args, err)
		os.Exit(1)
	}
}

func runCmdWithEnv(env []string, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "命令失败: %s %v: %v\n", name, args, err)
		os.Exit(1)
	}
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func copyDir(src, dst string) {
	os.MkdirAll(dst, 0755)
	entries, err := os.ReadDir(src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "copyDir 读取失败 %s: %v\n", src, err)
		os.Exit(1)
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.IsDir() {
			copyDir(srcPath, dstPath)
			continue
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "读取失败 %s: %v\n", srcPath, err)
			os.Exit(1)
		}
		if err := os.WriteFile(dstPath, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "写入失败 %s: %v\n", dstPath, err)
			os.Exit(1)
		}
	}
}
