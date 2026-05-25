package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/alecthomas/kong"
	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const workspace = "/workspace"

var (
	buildVersion string
	buildCommit  string
	buildDate    string
)

type cli struct {
	Host               string        `default:"0.0.0.0" env:"RCE_MCP_HOST" help:"HTTP listen host."`
	Port               int           `default:"3000" env:"RCE_MCP_PORT" help:"HTTP listen port."`
	AuthMode           string        `default:"token" enum:"token,none" env:"RCE_MCP_AUTH_MODE" help:"Authentication mode."`
	Token              string        `env:"RCE_MCP_TOKEN" help:"Bearer token for token auth."`
	DefaultTimeout     time.Duration `default:"30s" env:"RCE_MCP_DEFAULT_TIMEOUT" help:"Default command timeout."`
	MaxTimeout         time.Duration `default:"5m" env:"RCE_MCP_MAX_TIMEOUT" help:"Maximum command timeout."`
	DefaultOutputLimit int64         `default:"1048576" env:"RCE_MCP_DEFAULT_OUTPUT_LIMIT" help:"Default captured bytes per output stream."`
	MaxOutputLimit     int64         `default:"10485760" env:"RCE_MCP_MAX_OUTPUT_LIMIT" help:"Maximum captured bytes per output stream."`
	MaxStdin           int64         `default:"10485760" env:"RCE_MCP_MAX_STDIN" help:"Maximum stdin bytes."`
	MaxConcurrency     int           `default:"4" env:"RCE_MCP_MAX_CONCURRENCY" help:"Maximum concurrent commands."`
	Version            bool          `help:"Print version and exit."`
}

type ExecuteCommandInput struct {
	Program          string   `json:"program" jsonschema:"required"`
	Args             []string `json:"args,omitempty"`
	Stdin            string   `json:"stdin,omitempty"`
	TimeoutMS        int64    `json:"timeout_ms,omitempty"`
	OutputLimitBytes int64    `json:"output_limit_bytes,omitempty"`
}

type ExecuteBashInput struct {
	Script           string `json:"script" jsonschema:"required"`
	Stdin            string `json:"stdin,omitempty"`
	TimeoutMS        int64  `json:"timeout_ms,omitempty"`
	OutputLimitBytes int64  `json:"output_limit_bytes,omitempty"`
}

type CommandResult struct {
	ExitCode        *int   `json:"exit_code"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	StdoutTruncated bool   `json:"stdout_truncated"`
	StderrTruncated bool   `json:"stderr_truncated"`
	TimedOut        bool   `json:"timed_out"`
	DurationMS      int64  `json:"duration_ms"`
}

type limits struct {
	DefaultTimeout     time.Duration
	MaxTimeout         time.Duration
	DefaultOutputLimit int64
	MaxOutputLimit     int64
	MaxStdin           int64
}

type executor struct {
	workspace string
	limits    limits
	sem       chan struct{}
	env       []string
}

var errBusy = errors.New("concurrency limit reached")

func main() {
	zerolog.TimeFieldFormat = time.RFC3339

	var c cli
	kong.Parse(&c, kong.Name("rce-mcp"), kong.Description("MCP server for command execution."))

	if c.Version {
		fmt.Printf("Version: %s\nCommit:  %s\nDate:    %s\n", buildVersion, buildCommit, buildDate)
		return
	}

	if err := c.validate(); err != nil {
		log.Fatal().Err(err).Msg("invalid configuration")
	}

	exec := newExecutor(workspace, limits{
		DefaultTimeout:     c.DefaultTimeout,
		MaxTimeout:         c.MaxTimeout,
		DefaultOutputLimit: c.DefaultOutputLimit,
		MaxOutputLimit:     c.MaxOutputLimit,
		MaxStdin:           c.MaxStdin,
	}, c.MaxConcurrency)

	mcpSrv := mcpserver.NewMCPServer("rce-mcp", buildVersion)
	registerTools(mcpSrv, exec)

	mcpHTTP := mcpserver.NewStreamableHTTPServer(mcpSrv, mcpserver.WithStateLess(true))

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.Handle("/mcp", authMiddleware(c.AuthMode, c.Token, mcpHTTP))

	httpSrv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", c.Host, c.Port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info().Str("addr", httpSrv.Addr).Str("auth_mode", c.AuthMode).Msg("starting server")
		errCh <- httpSrv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Info().Str("signal", sig.String()).Msg("shutting down")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(ctx); err != nil {
			log.Error().Err(err).Msg("graceful shutdown failed")
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal().Err(err).Msg("server failed")
		}
	}
}

func (c cli) validate() error {
	if c.AuthMode == "token" && c.Token == "" {
		return errors.New("token is required when auth-mode is token")
	}
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("port out of range: %d", c.Port)
	}
	if c.DefaultTimeout <= 0 || c.MaxTimeout <= 0 || c.DefaultTimeout > c.MaxTimeout {
		return errors.New("timeouts must be positive and default-timeout must be <= max-timeout")
	}
	if c.DefaultOutputLimit <= 0 || c.MaxOutputLimit <= 0 || c.DefaultOutputLimit > c.MaxOutputLimit {
		return errors.New("output limits must be positive and default-output-limit must be <= max-output-limit")
	}
	if c.MaxStdin < 0 {
		return errors.New("max-stdin must be non-negative")
	}
	if c.MaxConcurrency <= 0 {
		return errors.New("max-concurrency must be positive")
	}
	return nil
}

func authMiddleware(mode, token string, next http.Handler) http.Handler {
	if mode == "none" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func registerTools(s *mcpserver.MCPServer, e *executor) {
	s.AddTool(mcp.NewTool("execute_command",
		mcp.WithDescription("Execute a program with arguments."),
		mcp.WithInputSchema[ExecuteCommandInput](),
		mcp.WithOutputSchema[CommandResult](),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var in ExecuteCommandInput
		if err := req.BindArguments(&in); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		res, err := e.execute(ctx, in.Program, in.Args, in.Stdin, in.TimeoutMS, in.OutputLimitBytes)
		return resultOrToolError(res, err)
	})

	s.AddTool(mcp.NewTool("execute_bash",
		mcp.WithDescription("Execute a Bash script."),
		mcp.WithInputSchema[ExecuteBashInput](),
		mcp.WithOutputSchema[CommandResult](),
	), func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var in ExecuteBashInput
		if err := req.BindArguments(&in); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		res, err := e.execute(ctx, "bash", []string{"-c", in.Script}, in.Stdin, in.TimeoutMS, in.OutputLimitBytes)
		return resultOrToolError(res, err)
	})
}

func resultOrToolError(res CommandResult, err error) (*mcp.CallToolResult, error) {
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultStructuredOnly(res), nil
}

func newExecutor(workspace string, lim limits, maxConcurrency int) *executor {
	return &executor{
		workspace: workspace,
		limits:    lim,
		sem:       make(chan struct{}, maxConcurrency),
		env:       commandEnv(),
	}
}

func commandEnv() []string {
	allowed := []string{"PATH", "HOME", "TERM", "LANG", "LC_ALL"}
	env := make([]string, 0, len(allowed))
	seenPath := false
	for _, key := range allowed {
		if val, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+val)
			if key == "PATH" {
				seenPath = true
			}
		}
	}
	if !seenPath {
		env = append(env, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	}
	return env
}

func (e *executor) execute(ctx context.Context, program string, args []string, stdin string, timeoutMS int64, outputLimitBytes int64) (CommandResult, error) {
	if err := validateProgramAndArgs(program, args); err != nil {
		return CommandResult{}, err
	}
	if int64(len(stdin)) > e.limits.MaxStdin {
		return CommandResult{}, fmt.Errorf("stdin exceeds max of %d bytes", e.limits.MaxStdin)
	}
	timeout := e.limits.DefaultTimeout
	if timeoutMS > 0 {
		timeout = time.Duration(timeoutMS) * time.Millisecond
	}
	if timeout <= 0 || timeout > e.limits.MaxTimeout {
		return CommandResult{}, fmt.Errorf("timeout must be > 0 and <= %s", e.limits.MaxTimeout)
	}
	outputLimit := e.limits.DefaultOutputLimit
	if outputLimitBytes > 0 {
		outputLimit = outputLimitBytes
	}
	if outputLimit <= 0 || outputLimit > e.limits.MaxOutputLimit {
		return CommandResult{}, fmt.Errorf("output_limit_bytes must be > 0 and <= %d", e.limits.MaxOutputLimit)
	}

	select {
	case e.sem <- struct{}{}:
		defer func() { <-e.sem }()
	default:
		return CommandResult{}, errBusy
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	//nolint:gosec // This server intentionally executes client-requested programs inside the container boundary.
	cmd := exec.Command(program, args...)
	cmd.Dir = e.workspace
	cmd.Env = e.env
	cmd.Stdin = strings.NewReader(stdin)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout := &limitedBuffer{limit: outputLimit}
	stderr := &limitedBuffer{limit: outputLimit}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	started := time.Now()
	if err := cmd.Start(); err != nil {
		return CommandResult{}, err
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	var waitErr error
	timedOut := false
	select {
	case waitErr = <-done:
	case <-ctx.Done():
		timedOut = true
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		waitErr = <-done
	}

	result := CommandResult{
		Stdout:          stdout.String(),
		Stderr:          stderr.String(),
		StdoutTruncated: stdout.truncated,
		StderrTruncated: stderr.truncated,
		TimedOut:        timedOut,
		DurationMS:      time.Since(started).Milliseconds(),
	}
	if !timedOut {
		code, err := exitCode(waitErr)
		if err != nil {
			return CommandResult{}, err
		}
		result.ExitCode = &code
	}

	log.Info().Str("tool_event", "command_complete").Int64("duration_ms", result.DurationMS).Bool("timed_out", result.TimedOut).Bool("stdout_truncated", result.StdoutTruncated).Bool("stderr_truncated", result.StderrTruncated).Any("exit_code", result.ExitCode).Msg("command completed")
	return result, nil
}

func validateProgramAndArgs(program string, args []string) error {
	if program == "" {
		return errors.New("program is required")
	}
	if strings.ContainsRune(program, '\x00') {
		return errors.New("program contains NUL byte")
	}
	for i, arg := range args {
		if strings.ContainsRune(arg, '\x00') {
			return fmt.Errorf("args[%d] contains NUL byte", i)
		}
	}
	return nil
}

func exitCode(err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return 0, err
}

type limitedBuffer struct {
	mu        sync.Mutex
	buf       bytes.Buffer
	limit     int64
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := b.limit - int64(b.buf.Len())
	if remaining <= 0 {
		b.truncated = true
		return len(p), nil
	}
	if int64(len(p)) > remaining {
		_, _ = b.buf.Write(p[:remaining])
		b.truncated = true
		return len(p), nil
	}
	_, _ = b.buf.Write(p)
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

var _ io.Writer = (*limitedBuffer)(nil)
