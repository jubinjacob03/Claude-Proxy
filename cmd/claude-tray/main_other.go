//go:build !windows

package main

import "fmt"

func main() {
	fmt.Println("claude-tray is a Windows-only application.")
}
