package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultServer = "http://localhost:8080"

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "push":
		cmdPush(args)
	case "pull":
		cmdPull(args)
	case "list":
		cmdList(args)
	case "search":
		cmdSearch(args)
	case "delete":
		cmdDelete(args)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`Foundry Registry CLI

Usage:
  registry push <package> <version> <file> [options]
  registry pull <package> <version> [options]
  registry list [options]
  registry search <query> [options]
  registry delete <package> <version> [options]

Options:
  --server <url>    Server URL (default: http://localhost:8080)
  --token <token>   Authentication token
  --output <file>   Output file path (for pull)`)
}

// parseFlags extracts --key value pairs from args.
func parseFlags(args []string) (positional []string, flags map[string]string) {
	flags = make(map[string]string)
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--") && i+1 < len(args) {
			flags[strings.TrimPrefix(args[i], "--")] = args[i+1]
			i++
		} else {
			positional = append(positional, args[i])
		}
	}
	return
}

func getFlag(flags map[string]string, key, def string) string {
	if v, ok := flags[key]; ok {
		return v
	}
	return def
}

func requireToken(flags map[string]string) string {
	token := getFlag(flags, "token", "")
	if token == "" {
		fmt.Fprintln(os.Stderr, "error: --token is required")
		os.Exit(1)
	}
	return token
}

func cmdPush(args []string) {
	pos, flags := parseFlags(args)
	if len(pos) < 3 {
		fmt.Fprintln(os.Stderr, "usage: registry push <package> <version> <file> [--server URL] [--token TOKEN]")
		os.Exit(1)
	}

	pkg, version, filePath := pos[0], pos[1], pos[2]
	server := getFlag(flags, "server", defaultServer)
	token := requireToken(flags)

	file, err := os.Open(filePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error opening file: %v\n", err)
		os.Exit(1)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading file info: %v\n", err)
		os.Exit(1)
	}

	// Create a progress reader.
	pr := &progressReader{
		reader: file,
		total:  info.Size(),
		label:  "Uploading",
	}

	req, err := http.NewRequest("POST", artifactURL(server, pkg, version), pr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating request: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = info.Size()

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()
	fmt.Println() // newline after progress

	if resp.StatusCode != http.StatusCreated {
		fmt.Fprintln(os.Stderr, formatHTTPError(resp))
		os.Exit(1)
	}

	var result struct {
		Hash string `json:"hash"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Fprintf(os.Stderr, "error decoding response: %v\n", err)
		os.Exit(1)
	}
	elapsed := time.Since(start)

	fmt.Printf("Pushed %s@%s\n", pkg, version)
	fmt.Printf("  Hash:     %s\n", result.Hash)
	fmt.Printf("  Size:     %s\n", formatBytes(info.Size()))
	fmt.Printf("  Duration: %v\n", elapsed.Round(time.Millisecond))
}

func cmdPull(args []string) {
	pos, flags := parseFlags(args)
	if len(pos) < 2 {
		fmt.Fprintln(os.Stderr, "usage: registry pull <package> <version> [--server URL] [--token TOKEN] [--output FILE]")
		os.Exit(1)
	}

	pkg, version := pos[0], pos[1]
	server := getFlag(flags, "server", defaultServer)
	token := requireToken(flags)
	output := getFlag(flags, "output", fmt.Sprintf("%s-%s", pkg, version))

	req, err := http.NewRequest("GET", artifactURL(server, pkg, version), nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating request: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, formatHTTPError(resp))
		os.Exit(1)
	}

	outputDir := filepath.Dir(output)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "error creating output directory: %v\n", err)
		os.Exit(1)
	}

	tmpOutput := output + ".part"
	file, err := os.Create(tmpOutput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating output file: %v\n", err)
		os.Exit(1)
	}
	success := false
	defer func() {
		file.Close()
		if !success {
			_ = os.Remove(tmpOutput)
		}
	}()

	pr := &progressWriter{
		writer: file,
		total:  resp.ContentLength,
		label:  "Downloading",
	}

	start := time.Now()
	n, err := io.Copy(pr, resp.Body)
	fmt.Println() // newline after progress
	if err != nil {
		fmt.Fprintf(os.Stderr, "error downloading: %v\n", err)
		os.Exit(1)
	}
	if err := file.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "error closing downloaded file: %v\n", err)
		os.Exit(1)
	}
	if err := os.Remove(output); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "error replacing output file: %v\n", err)
		os.Exit(1)
	}
	if err := os.Rename(tmpOutput, output); err != nil {
		fmt.Fprintf(os.Stderr, "error finalizing output file: %v\n", err)
		os.Exit(1)
	}
	success = true

	elapsed := time.Since(start)
	fmt.Printf("Pulled %s@%s -> %s\n", pkg, version, output)
	fmt.Printf("  Hash:     %s\n", resp.Header.Get("X-Artifact-Hash"))
	fmt.Printf("  Size:     %s\n", formatBytes(n))
	fmt.Printf("  Duration: %v\n", elapsed.Round(time.Millisecond))
}

func cmdList(args []string) {
	_, flags := parseFlags(args)
	server := getFlag(flags, "server", defaultServer)
	token := requireToken(flags)

	req, _ := http.NewRequest("GET", packagesURL(server), nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, formatHTTPError(resp))
		os.Exit(1)
	}

	var packages []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&packages); err != nil {
		fmt.Fprintf(os.Stderr, "error decoding response: %v\n", err)
		os.Exit(1)
	}

	if len(packages) == 0 {
		fmt.Println("No packages found.")
		return
	}

	fmt.Println("Packages:")
	for _, p := range packages {
		fmt.Printf("  - %v\n", p["name"])
	}
}

func cmdSearch(args []string) {
	pos, flags := parseFlags(args)
	if len(pos) < 1 {
		fmt.Fprintln(os.Stderr, "usage: registry search <query> [--server URL] [--token TOKEN]")
		os.Exit(1)
	}

	query := pos[0]
	server := getFlag(flags, "server", defaultServer)
	token := requireToken(flags)

	req, _ := http.NewRequest("GET", searchURL(server, query), nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, formatHTTPError(resp))
		os.Exit(1)
	}

	var packages []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&packages); err != nil {
		fmt.Fprintf(os.Stderr, "error decoding response: %v\n", err)
		os.Exit(1)
	}

	if len(packages) == 0 {
		fmt.Printf("No packages matching '%s'.\n", query)
		return
	}

	fmt.Printf("Search results for '%s':\n", query)
	for _, p := range packages {
		fmt.Printf("  - %v\n", p["name"])
	}
}

func cmdDelete(args []string) {
	pos, flags := parseFlags(args)
	if len(pos) < 2 {
		fmt.Fprintln(os.Stderr, "usage: registry delete <package> <version> [--server URL] [--token TOKEN]")
		os.Exit(1)
	}

	pkg, version := pos[0], pos[1]
	server := getFlag(flags, "server", defaultServer)
	token := requireToken(flags)

	req, _ := http.NewRequest("DELETE", artifactURL(server, pkg, version), nil)
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, formatHTTPError(resp))
		os.Exit(1)
	}

	fmt.Printf("Deleted %s@%s\n", pkg, version)
}

// progressReader wraps a reader and prints progress.
type progressReader struct {
	reader  io.Reader
	total   int64
	current int64
	label   string
}

func (pr *progressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	pr.current += int64(n)
	pr.printProgress()
	return n, err
}

func (pr *progressReader) printProgress() {
	if pr.total <= 0 {
		fmt.Fprintf(os.Stderr, "\r%s: %s", pr.label, formatBytes(pr.current))
		return
	}
	pct := float64(pr.current) / float64(pr.total) * 100
	barLen := 30
	filled := int(pct / 100 * float64(barLen))
	bar := strings.Repeat("=", filled) + strings.Repeat(" ", barLen-filled)
	fmt.Fprintf(os.Stderr, "\r%s: [%s] %.1f%% %s/%s", pr.label, bar, pct, formatBytes(pr.current), formatBytes(pr.total))
}

// progressWriter wraps a writer and prints progress.
type progressWriter struct {
	writer  io.Writer
	total   int64
	current int64
	label   string
}

func (pw *progressWriter) Write(p []byte) (int, error) {
	n, err := pw.writer.Write(p)
	pw.current += int64(n)
	pw.printProgress()
	return n, err
}

func (pw *progressWriter) printProgress() {
	if pw.total <= 0 {
		fmt.Fprintf(os.Stderr, "\r%s: %s", pw.label, formatBytes(pw.current))
		return
	}
	pct := float64(pw.current) / float64(pw.total) * 100
	barLen := 30
	filled := int(pct / 100 * float64(barLen))
	bar := strings.Repeat("=", filled) + strings.Repeat(" ", barLen-filled)
	fmt.Fprintf(os.Stderr, "\r%s: [%s] %.1f%% %s/%s", pw.label, bar, pct, formatBytes(pw.current), formatBytes(pw.total))
}

func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func artifactURL(server, pkg, version string) string {
	return fmt.Sprintf("%s/api/v1/artifacts/%s/%s", strings.TrimRight(server, "/"), url.PathEscape(pkg), url.PathEscape(version))
}

func packagesURL(server string) string {
	return fmt.Sprintf("%s/api/v1/packages", strings.TrimRight(server, "/"))
}

func searchURL(server, query string) string {
	return fmt.Sprintf("%s/api/v1/packages?search=%s", strings.TrimRight(server, "/"), url.QueryEscape(query))
}

func formatHTTPError(resp *http.Response) string {
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		return fmt.Sprintf("error (%d): %s", resp.StatusCode, http.StatusText(resp.StatusCode))
	}

	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Message != "" {
		return fmt.Sprintf("error (%d): %s", resp.StatusCode, payload.Message)
	}
	return fmt.Sprintf("error (%d): %s", resp.StatusCode, strings.TrimSpace(string(body)))
}
