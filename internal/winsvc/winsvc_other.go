//go:build !windows

// Package winsvc: fuera de Windows no hay servicio; el agente corre en primer plano (`run`).
package winsvc

import (
	"context"
	"errors"
)

const Name = "ZertixPOSAgent"

func IsService() bool { return false }

func Run(run func(ctx context.Context) error) error {
	return errors.New("el servicio solo existe en Windows: usa `run`")
}

func Install(exePath string) error { return errors.New("solo en Windows") }

func Uninstall() error { return errors.New("solo en Windows") }

func Installed() bool { return false }
