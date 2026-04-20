package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/edgefn/http-mock/pkg/server"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatalf("http-mock: %v", err)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return usageError()
	}

	switch args[0] {
	case "serve":
		return runServe(args[1:])
	case "validate":
		return runValidate(args[1:])
	case "-h", "--help", "help":
		return usageError()
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usageText())
	}
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	routesPath := fs.String("routes", "", "routes yaml path; relative paths are resolved under --data-root")
	profilePath := fs.String("profile", "", "deprecated alias of --routes")
	dataRoot := fs.String("data-root", ".", "mock data root")
	listen := fs.String("listen", ":18080", "listen address")
	readHeaderTimeout := fs.Duration("read-header-timeout", 10*time.Second, "server read header timeout")

	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedRoutes := firstNonEmpty(*routesPath, *profilePath)
	if resolvedRoutes == "" {
		return errors.New("--routes is required")
	}

	srv, err := server.Load(*dataRoot, resolvedRoutes)
	if err != nil {
		return err
	}

	httpSrv := &http.Server{
		Addr:              *listen,
		Handler:           srv,
		ReadHeaderTimeout: *readHeaderTimeout,
	}
	log.Printf("http-mock serving routes=%q data_root=%q listen=%q", resolvedRoutes, *dataRoot, *listen)

	errCh := make(chan error, 1)
	go func() {
		errCh <- httpSrv.ListenAndServe()
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-sigCh:
		log.Printf("http-mock shutting down after signal=%s", sig)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return httpSrv.Shutdown(ctx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	routesPath := fs.String("routes", "", "routes yaml path; relative paths are resolved under --data-root")
	profilePath := fs.String("profile", "", "deprecated alias of --routes")
	dataRoot := fs.String("data-root", ".", "mock data root")

	if err := fs.Parse(args); err != nil {
		return err
	}
	resolvedRoutes := firstNonEmpty(*routesPath, *profilePath)
	if resolvedRoutes == "" {
		return errors.New("--routes is required")
	}

	if _, err := server.Load(*dataRoot, resolvedRoutes); err != nil {
		return err
	}
	fmt.Printf("routes %q is valid\n", resolvedRoutes)
	return nil
}

func usageError() error {
	return errors.New(usageText())
}

func usageText() string {
	return `Usage:
  http-mock serve --routes <routes-file> --data-root <dir> --listen :18080
  http-mock validate --routes <routes-file> --data-root <dir>`
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
