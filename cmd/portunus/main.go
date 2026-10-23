package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/darshmahadevia/portunus/internal/fakeprovider"
	"github.com/darshmahadevia/portunus/internal/gateway"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stderr); err != nil {
		log.Printf("portunus: %v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("portunus", flag.ContinueOnError)
	flags.SetOutput(output)
	gatewayAddress := flags.String("gateway-listen", "127.0.0.1:8080", "gateway loopback address")
	fakeAddress := flags.String("fake-listen", "127.0.0.1:8081", "fake provider loopback address")
	timeout := flags.Duration("timeout", 30*time.Second, "total gateway chat request timeout")
	config := fakeprovider.DefaultConfig()
	flags.DurationVar(&config.ChunkDelay, "fake-chunk-delay", config.ChunkDelay, "delay between fake chunks")
	flags.DurationVar(&config.FirstChunkDelay, "fake-first-chunk-delay", 0, "delay before the fake response begins")
	flags.IntVar(&config.Status, "fake-status", config.Status, "fake HTTP status, 200, 429, or 503")
	flags.IntVar(&config.CutAfter, "fake-cut-after", config.CutAfter, "abort after N deltas, -1 disables; JSON aborts before headers")
	chunks := flags.String("fake-chunks", strings.Join(config.Chunks, "|"), "deterministic fake chunks separated by |")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %s", strings.Join(flags.Args(), " "))
	}
	if *timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	for _, address := range []string{*gatewayAddress, *fakeAddress} {
		if err := validateAddress(address); err != nil {
			return err
		}
	}
	config.Chunks = strings.Split(*chunks, "|")
	fake, err := fakeprovider.New(config)
	if err != nil {
		return err
	}
	fakeListener, err := net.Listen("tcp", *fakeAddress)
	if err != nil {
		return fmt.Errorf("listen fake provider %s: %w", *fakeAddress, err)
	}
	defer fakeListener.Close()
	gatewayListener, err := net.Listen("tcp", *gatewayAddress)
	if err != nil {
		return fmt.Errorf("listen gateway %s: %w", *gatewayAddress, err)
	}
	defer gatewayListener.Close()
	proxy, err := gateway.New(gateway.Config{ProviderURL: "http://" + fakeListener.Addr().String(), Timeout: *timeout})
	if err != nil {
		return err
	}
	baseContext, cancel := context.WithCancel(ctx)
	defer cancel()
	servers := []*http.Server{
		newServer(baseContext, proxy),
		newServer(baseContext, fake),
	}
	listeners := []net.Listener{gatewayListener, fakeListener}
	results := make(chan error, len(servers))
	for i, server := range servers {
		go func(server *http.Server, listener net.Listener) {
			results <- server.Serve(listener)
		}(server, listeners[i])
	}
	log.New(output, "", 0).Printf("ready gateway=http://%s fake=http://%s", gatewayListener.Addr(), fakeListener.Addr())
	var serveError error
	completed := 0
	select {
	case <-ctx.Done():
	case serveError = <-results:
		completed++
	}
	cancel()
	shutdownContext, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	var shutdownError error
	for _, server := range servers {
		if err := server.Shutdown(shutdownContext); err != nil {
			shutdownError = errors.Join(shutdownError, err)
			_ = server.Close()
		}
	}
	for completed < len(servers) {
		err := <-results
		completed++
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveError = errors.Join(serveError, err)
		}
	}
	if errors.Is(serveError, http.ErrServerClosed) {
		serveError = nil
	}
	return errors.Join(serveError, shutdownError)
}

func newServer(ctx context.Context, handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}

func validateAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	number, err := strconv.Atoi(port)
	if ip == nil || !ip.IsLoopback() || err != nil || number < 0 || number > 65535 {
		return fmt.Errorf("listen address %q must use a loopback IP and port 0-65535", address)
	}
	return nil
}
