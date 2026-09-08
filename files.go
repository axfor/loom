package loom

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Ext 是模板的扩展名 —— 与设置文件同一个后缀：**同一种语法只该有一个后缀**。
// 设置文件靠名字（loom.lm）认，模板靠所在目录认。
const Ext = ".lm"

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
//
// 【一个产物只该有一份模板】两份模板写同一个 target，谁生效取决于枚举顺序 ——
// "我改了模板却没生效"会变成一个查不出原因的谜。这条只有织机看得见（它是唯一
// 读全部模板的东西），所以由它拒绝；让下游的门各自写正则去补，就会出现
// "那道门按产物路径做键、重复的早被合并掉"这种结构上不可能触发的检查。
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
	seen := map[string]string{}
	for _, p := range out {
		t, err := LoadTemplate(p)
		if err != nil {
			continue // 解析错误由各自的调用方报，这里只管重名
		}
		if first, dup := seen[t.Target]; dup {
			return nil, fmt.Errorf("%s 有两份模板：%s 与 %s —— 一个产物只该有一份，"+
				"谁生效取决于枚举顺序", t.Target, rel(c, first), rel(c, p))
		}
		seen[t.Target] = p
	}
	return out, nil
}

func rel(c *Config, p string) string {
	if r, err := filepath.Rel(c.Root, p); err == nil {
		return r
	}
	return p
}
