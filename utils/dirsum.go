package utils

import (
	"hash"
	"encoding/hex"
	"crypto/sha256"
	"path/filepath"
	"os"
	"strconv"
	"strings"
)

type DirSum struct {
	sums               map[string]string
	path               string
}

func (ds *DirSum) GetSums() map[string]string {
	return ds.sums
}

func (ds *DirSum) encodeFileInfo(h hash.Hash, info os.FileInfo) error {
	for _, elem := range [][2]string{
		{"name", info.Name()},
		{"size", strconv.Itoa(int(info.Size()))},
		{"mode", strconv.Itoa(int(info.Mode()))},
		{"mtime", strconv.Itoa(int(info.ModTime().UTC().Unix()))},
	} {
		if _, err := h.Write([]byte(elem[0] + elem[1])); err != nil {
			return err
		}
	}
	return nil
}

func (ds *DirSum) compute(path string, info os.FileInfo, err error) error {
	if err != nil {
		return err
	}

	if !strings.HasPrefix(path, ds.path) {
		panic("unreachable")
	}

	if path == ds.path {
		// ignore the root node because its mtime changes in every build
		return nil
	}

	currentFile := path[len(ds.path):]

	h := sha256.New()
	ds.encodeFileInfo(h, info)
	ds.sums[currentFile] = hex.EncodeToString(h.Sum(nil))

	return nil
}

func NewDirSum(path string) *DirSum {

	ds := &DirSum{
		path: strings.TrimRight(path, "/")+"/",
		sums: make(map[string]string),
	}
	filepath.Walk(ds.path, ds.compute)
	return ds
}
