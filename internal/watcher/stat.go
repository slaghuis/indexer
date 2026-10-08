package watcher

import "os"

func statFile(p string) (os.FileInfo, error) { return os.Stat(p) }