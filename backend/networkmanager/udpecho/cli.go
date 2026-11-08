package udpecho

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"
)

// RunCLI exposes the former standalone UDP scripts without an interpreter.
// args begins with test or responder; all flags use ordinary Go CLI spelling.
func RunCLI(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: udp-echo test -server IPv4 | udp-echo responder [-bind IPv4]")
	}

	flags := flag.NewFlagSet("udp-echo "+args[0], flag.ContinueOnError)
	flags.SetOutput(out)
	port := flags.Int("port", 39091, "UDP port (1024..65535)")
	seconds := flags.Int("duration-seconds", 0, "Duration (responder: 0 runs until Ctrl+C; test default: 60)")
	switch args[0] {
	case "responder":
		bind := flags.String("bind", "0.0.0.0", "Local IPv4 address")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}

		if flags.NArg() != 0 || *port < 1024 || *port > 65535 || *seconds < 0 || *seconds > 86400 {
			return errors.New("invalid responder port or duration")
		}

		return RunResponder(ctx, ResponderConfig{
			BindAddress: *bind,
			Port:        *port,
			Duration:    time.Duration(*seconds) * time.Second,
		}, out)
	case "test":
		var cfg TestConfig
		flags.StringVar(&cfg.Server, "server", "", "Your controlled responder IPv4 or hostname")
		flags.StringVar(&cfg.AdapterName, "adapter", "", "Exact Windows adapter name; Loopback is for local QA")
		flags.StringVar(&cfg.Source, "source", "", "Explicit local IPv4 (requires interface-index)")
		flags.IntVar(&cfg.InterfaceIndex, "interface-index", 0, "Source interface index")
		flags.IntVar(&cfg.PPS, "pps", 60, "Datagrams per second (1..120)")
		flags.IntVar(&cfg.PayloadBytes, "payload-bytes", 256, "Datagram size (32..1200)")
		flags.StringVar(&cfg.LogDirectory, "logs", "", "Output directory")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}

		if *seconds == 0 {
			*seconds = 60
		}

		if flags.NArg() != 0 || cfg.Server == "" || *port < 1024 || *port > 65535 || *seconds < 10 || *seconds > 600 || cfg.PPS < 1 || cfg.PPS > 120 || cfg.PayloadBytes < 32 || cfg.PayloadBytes > 1200 {
			return errors.New("invalid test flags: require server, duration 10..600s, PPS 1..120 and payload 32..1200")
		}

		cfg.Port, cfg.Duration = *port, time.Duration(*seconds)*time.Second
		path, err := Test(ctx, cfg, out)
		if path != "" {
			fmt.Fprintln(out, "Report:", path)
		}

		return err
	default:
		return fmt.Errorf("unknown UDP command %q", args[0])
	}
}
