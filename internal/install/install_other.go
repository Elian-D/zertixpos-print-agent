//go:build !windows

package install

import (
	"context"
	"errors"
)

var errWindows = errors.New("la instalación como servicio es solo para Windows: en Linux usa `pair` y `run`")

func Dir() string { return "" }

func ExePath() string { return "" }

func IsAdmin() bool { return true }

func Elevate(args []string) error { return errWindows }

func Installed() bool { return false }

func Run(ctx context.Context, central, version string) error { return errWindows }

func Uninstall(purge bool) error { return errWindows }
