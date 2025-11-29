package main

import (
	"github.com/topi314/tint"
	"log/slog"
	"os"
)

func reddactSensitiveInfo(groups []string, a slog.Attr) slog.Attr {
	if a.Key == "dsn" || a.Key == "access_token" || a.Key == "password" {
		a.Value = slog.StringValue("[REDACTED]")
	}
	return a
}

const (
	ansiFaint         = "\033[2m"
	ansiWhiteBold     = "\033[37;1m"
	ansiYellowBold    = "\033[33;1m"
	ansiCyanBold      = "\033[36;1m"
	ansiCyanBoldFaint = "\033[36;1;2m"
	ansiRedFaint      = "\033[31;2m"
	ansiRedBold       = "\033[31;1m"

	ansiRed     = "\033[31m"
	ansiYellow  = "\033[33m"
	ansiGreen   = "\033[32m"
	ansiMagenta = "\033[35m"
)

func SetupLogger(cfg LogConfig) {
	var handler slog.Handler
	switch cfg.Format {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			AddSource:   cfg.AddSource,
			Level:       cfg.Level,
			ReplaceAttr: reddactSensitiveInfo,
		})
	case "text":
		handler = tint.NewHandler(os.Stdout, &tint.Options{
			ReplaceAttr: reddactSensitiveInfo,
			AddSource:   cfg.AddSource,
			NoColor:     cfg.NoColor,
			Level:       cfg.Level,
			LevelColors: map[slog.Level]string{
				slog.LevelDebug: ansiMagenta,
				slog.LevelInfo:  ansiGreen,
				slog.LevelWarn:  ansiYellow,
				slog.LevelError: ansiRed,
			},
			Colors: map[tint.Kind]string{
				tint.KindTime:            ansiYellowBold,
				tint.KindSourceFile:      ansiCyanBold,
				tint.KindSourceSeparator: ansiCyanBoldFaint,
				tint.KindSourceLine:      ansiCyanBold,
				tint.KindMessage:         ansiWhiteBold,
				tint.KindKey:             ansiFaint,
				tint.KindSeparator:       ansiFaint,
				tint.KindValue:           ansiWhiteBold,
				tint.KindErrorKey:        ansiRedFaint,
				tint.KindErrorSeparator:  ansiFaint,
				tint.KindErrorValue:      ansiRedBold,
			},
		})

	default:
		slog.Error("Unsupported log format", "format", cfg.Format)
		os.Exit(-1)
	}
	slog.SetDefault(slog.New(handler))
}
