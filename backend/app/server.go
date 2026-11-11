package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

func Run(ctx context.Context, cfg Config) error {
	return RunWithDependencies(ctx, cfg, Dependencies{})
}

func RunWithDependencies(ctx context.Context, cfg Config, dependencies Dependencies) (runErr error) {
	a, err := New(ctx, cfg, dependencies)
	if err != nil {
		return err
	}

	defer func() {
		if err := a.Close(); runErr == nil {
			runErr = err
		}
	}()
	listener, err := a.listen()
	if err != nil {
		return err
	}

	handler, err := a.newHandler()
	if err != nil {
		listener.Close()

		return err
	}

	server := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       5 * time.Second,
		MaxHeaderBytes:    8192,
	}
	if err = a.publishState(a.manager.State()); err != nil {
		listener.Close()

		return err
	}

	if err = a.manager.Start(); err != nil {
		listener.Close()

		return err
	}

	a.manager.Event("start", "Monitoring started")
	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Serve(listener)
	}()
	ownerDone := make(chan struct{})
	go a.monitorOwner(ownerDone)
	select {
	case <-a.ctx.Done():
	case err = <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			a.cancel()

			return err
		}
	}

	a.cancel()
	<-ownerDone
	shutdown, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	return server.Shutdown(shutdown)
}

func (a *App) listen() (net.Listener, error) {
	var listener net.Listener
	var err error

	for port := a.cfg.Port; port < a.cfg.Port+20; port++ {
		listener, err = net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			a.mu.Lock()
			a.port = port
			a.mu.Unlock()
			break
		}
	}

	return listener, err
}
