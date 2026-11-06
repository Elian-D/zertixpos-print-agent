//go:build !windows

// Package console: en Linux la terminal ya está; solo se pregunta por stdin.
package console

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func Open(title string) bool { return false }

func Pause() {}

func Ask(prompt string) string {
	fmt.Print(prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line)
}
