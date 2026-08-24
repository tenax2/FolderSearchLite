//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"path/filepath"

	"golang.org/x/sys/windows"
)

// openSystemPath はWindows Shellへ対象を渡す。
// 通常ファイルには拡張子の関連付けが使われ、フォルダはExplorerで開かれる。
func openSystemPath(path string, isDirectory bool) error {
	if isDirectory {
		windowsDirectory, err := windows.GetWindowsDirectory()
		if err != nil {
			return fmt.Errorf("Windowsディレクトリを確認できません: %w", err)
		}
		cmd := exec.Command(filepath.Join(windowsDirectory, "explorer.exe"), path)
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("Explorerの起動に失敗しました: %w", err)
		}
		if err := cmd.Process.Release(); err != nil {
			return fmt.Errorf("Explorerのプロセスを解放できませんでした: %w", err)
		}
		return nil
	}

	operation, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, operation, target, nil, nil, windows.SW_SHOWNORMAL)
}
