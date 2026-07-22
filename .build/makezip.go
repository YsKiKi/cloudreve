package main

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	srcDir := filepath.Join("assets", "build")
	dstFile := filepath.Join("application", "statics", "assets.zip")

	os.Remove(dstFile)
	f, err := os.Create(dstFile)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	defer w.Close()

	err = filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// 从项目根目录计算相对路径，保留 assets/build/ 前缀
		// statics.go 中 fs.Sub(statics, "assets/build") 需要此前缀
		rel, err := filepath.Rel(".", path)
		if err != nil {
			return err
		}
		rel = strings.ReplaceAll(rel, "\\", "/")

		if info.IsDir() {
			if rel == srcDir {
				return nil // 跳过根目录自身
			}
			// 添加目录条目（zip 遍历需要）
			_, err := w.Create(rel + "/")
			return err
		}

		fmt.Println("adding:", rel)
		zf, err := w.Create(rel)
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		defer src.Close()
		_, err = io.Copy(zf, src)
		return err
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("Done!", dstFile)
}
