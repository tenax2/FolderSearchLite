//go:build !windows

package main

import (
	"fmt"
	"os/exec"
	"runtime"
)

// openSystemPath はWindows以外で、デスクトップ環境の標準オープナーへ対象を渡す。
func openSystemPath(path string, _ bool) error {
	command := "xdg-open"
	if runtime.GOOS == "darwin" {
		command = "open"
	}

	cmd := exec.Command(command, path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%sの起動に失敗しました: %w", command, err)
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("%sのプロセスを解放できませんでした: %w", command, err)
	}
	return nil
}
