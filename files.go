package loom

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Ext 是模板的扩展名。
const Ext = ".loom"

// LoadTemplate 读一份模板文件。
func LoadTemplate(path string) (*Template, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseTemplate(path, src)
}

// Templates 列出配置指向的模板目录里的全部模板，按路径排序 ——
// 顺序稳定，产物才可复现。
func Templates(c *Config) ([]string, error) {
	root := filepath.Join(c.Root, c.Templates)
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, Ext) {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}
