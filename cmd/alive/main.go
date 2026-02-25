package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type row struct {
	target string
	state  string
	code   int
	span   time.Duration
	size   int64
	issue  string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		printhelp()
		return nil
	}
	mode := args[0]
	switch mode {
	case "check":
		return runcheck(args[1:])
	case "file":
		return runfile(args[1:])
	case "serve":
		return runserve(args[1:])
	case "help":
		printhelp()
		return nil
	default:
		return fmt.Errorf("unknown mode: %s", mode)
	}
}

func runcheck(args []string) error {
	if len(args) == 0 {
		return errors.New("missing urls")
	}
	urls, span, err := spliturls(args, 3500*time.Millisecond)
	if err != nil {
		return err
	}
	rows := checkmany(urls, span)
	fmt.Print(render(rows))
	return nil
}

func runfile(args []string) error {
	if len(args) == 0 {
		return errors.New("missing file path")
	}
	path := args[0]
	span := 3500 * time.Millisecond
	if len(args) > 1 {
		part, err := parsems(args[1])
		if err != nil {
			return err
		}
		span = part
	}
	urls, err := load(path)
	if err != nil {
		return err
	}
	if len(urls) == 0 {
		return errors.New("no urls in file")
	}
	rows := checkmany(urls, span)
	fmt.Print(render(rows))
	return nil
}

func runserve(args []string) error {
	port := "4177"
	span := 3500 * time.Millisecond
	if len(args) > 0 {
		port = args[0]
	}
	if len(args) > 1 {
		part, err := parsems(args[1])
		if err != nil {
			return err
		}
		span = part
	}
	addr := ":" + port
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "alive")
		fmt.Fprintln(w, "")
		fmt.Fprintln(w, "try:")
		fmt.Fprintln(w, "  /check?url=https://example.com")
		fmt.Fprintln(w, "  /check?url=https://example.com&url=https://go.dev")
		fmt.Fprintln(w, "  /check?url=https://example.com&timeout=1200")
	})
	mux.HandleFunc("/check", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()["url"]
		if len(query) == 0 {
			if one := strings.TrimSpace(r.URL.Query().Get("target")); one != "" {
				query = []string{one}
			}
		}
		if len(query) == 0 {
			http.Error(w, "missing url query", http.StatusBadRequest)
			return
		}
		used := span
		if raw := strings.TrimSpace(r.URL.Query().Get("timeout")); raw != "" {
			part, err := parsems(raw)
			if err != nil {
				http.Error(w, "invalid timeout", http.StatusBadRequest)
				return
			}
			used = part
		}
		rows := checkmany(query, used)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprint(w, render(rows))
	})
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
	}
	fmt.Printf("alive serving on %s\n", addr)
	return srv.ListenAndServe()
}

func spliturls(args []string, base time.Duration) ([]string, time.Duration, error) {
	if len(args) == 0 {
		return nil, 0, errors.New("missing urls")
	}
	span := base
	urls := args
	last := strings.TrimSpace(args[len(args)-1])
	if maybe(last) {
		part, err := parsems(last)
		if err != nil {
			return nil, 0, err
		}
		span = part
		urls = args[:len(args)-1]
	}
	if len(urls) == 0 {
		return nil, 0, errors.New("missing urls")
	}
	return urls, span, nil
}

func maybe(raw string) bool {
	if raw == "" {
		return false
	}
	for _, ch := range raw {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func parsems(raw string) (time.Duration, error) {
	count, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || count <= 0 {
		return 0, errors.New("timeout must be positive milliseconds")
	}
	if count > 120000 {
		return 0, errors.New("timeout too large")
	}
	return time.Duration(count) * time.Millisecond, nil
}

func load(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	set := map[string]struct{}{}
	scan := bufio.NewScanner(file)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		set[line] = struct{}{}
	}
	if err := scan.Err(); err != nil {
		return nil, err
	}
	list := make([]string, 0, len(set))
	for item := range set {
		list = append(list, item)
	}
	sort.Strings(list)
	return list, nil
}

func checkmany(input []string, span time.Duration) []row {
	urls := clean(input)
	rows := make([]row, len(urls))
	if len(urls) == 0 {
		return rows
	}
	count := len(urls)
	workers := 8
	if count < workers {
		workers = count
	}
	type job struct {
		index int
		item  string
	}
	queue := make(chan job)
	var wait sync.WaitGroup
	for i := 0; i < workers; i++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for task := range queue {
				rows[task.index] = check(task.item, span)
			}
		}()
	}
	for i, item := range urls {
		queue <- job{index: i, item: item}
	}
	close(queue)
	wait.Wait()
	return rows
}

func clean(input []string) []string {
	set := map[string]struct{}{}
	for _, raw := range input {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		set[item] = struct{}{}
	}
	list := make([]string, 0, len(set))
	for item := range set {
		list = append(list, item)
	}
	sort.Strings(list)
	return list
}

func check(item string, span time.Duration) row {
	used := strings.TrimSpace(item)
	if err := okurl(used); err != nil {
		return row{target: used, state: "invalid", issue: err.Error()}
	}
	ctx, stop := context.WithTimeout(context.Background(), span)
	defer stop()
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, used, nil)
	if err != nil {
		return row{target: used, state: "invalid", issue: err.Error()}
	}
	req.Header.Set("User-Agent", "alive/1")
	cli := &http.Client{Timeout: span}
	res, err := cli.Do(req)
	if err != nil {
		return row{target: used, state: "down", span: time.Since(start), issue: maperr(err)}
	}
	defer res.Body.Close()
	state := "up"
	if res.StatusCode >= 400 {
		state = "warn"
	}
	size := res.ContentLength
	if size < 0 {
		size = 0
	}
	return row{target: used, state: state, code: res.StatusCode, span: time.Since(start), size: size}
}

func okurl(raw string) error {
	part, err := url.ParseRequestURI(raw)
	if err != nil {
		return errors.New("bad url")
	}
	if part.Scheme != "http" && part.Scheme != "https" {
		return errors.New("scheme must be http or https")
	}
	if part.Host == "" {
		return errors.New("missing host")
	}
	if strings.Contains(part.Host, " ") {
		return errors.New("bad host")
	}
	if _, _, err := net.SplitHostPort(part.Host); err == nil {
		return nil
	}
	if strings.Count(part.Host, ":") > 1 && !strings.HasPrefix(part.Host, "[") {
		return errors.New("bad host")
	}
	return nil
}

func maperr(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "deadline exceeded") {
		return "timeout"
	}
	if strings.Contains(text, "no such host") {
		return "dns"
	}
	if strings.Contains(text, "connection refused") {
		return "refused"
	}
	if strings.Contains(text, "certificate") {
		return "tls"
	}
	return "error"
}

func render(rows []row) string {
	if len(rows) == 0 {
		return "no targets\n"
	}
	var b strings.Builder
	fmt.Fprintln(&b, "target\tstate\tcode\tlatency\tsize\tnote")
	for _, item := range rows {
		code := "-"
		if item.code > 0 {
			code = strconv.Itoa(item.code)
		}
		latency := "-"
		if item.span > 0 {
			latency = item.span.Round(time.Millisecond).String()
		}
		size := "-"
		if item.size > 0 {
			size = strconv.FormatInt(item.size, 10)
		}
		note := "-"
		if item.issue != "" {
			note = item.issue
		}
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%s\n", item.target, item.state, code, latency, size, note)
	}
	return b.String()
}

func printhelp() {
	fmt.Println("alive")
	fmt.Println("")
	fmt.Println("usage:")
	fmt.Println("  alive check <url> [url...] [timeoutms]")
	fmt.Println("  alive file <path> [timeoutms]")
	fmt.Println("  alive serve [port] [timeoutms]")
}
