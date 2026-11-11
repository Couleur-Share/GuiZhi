//go:build !windows

package main

import "errors"

func safeStorageDecrypt(string, []byte) ([]byte, error) {
	return nil, errors.New("safeStorage 互通只实现了 Windows")
}
func safeStorageEncrypt(string, []byte) ([]byte, error) {
	return nil, errors.New("safeStorage 互通只实现了 Windows")
}
