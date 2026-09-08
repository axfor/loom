// loom —— 把两层源织成一个产物。
//
// 经线（warp）是上游那一层：逐字节保留，一根不断。
// 纬线（weft）是你自己那一层：一梭梭穿进经线之间。
// 抽掉纬线，经线原样还在 —— 这条性质可以机械验证，也正是这门语言存在的理由。
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/axfor/loom"
)

const usage = `loom —— 把两层源织成一个产物

用法：
  loom weave <模板.lm>      织一份，产物写到标准输出
  loom build                  织全部模板，写到各自的 target
  loom list                   把每份模板的元信息按 JSON 吐出来（给外层的门用）
  loom list -tsv              同上，只出六列纯文本：产物 / 模板 / 类型 / 基底层 / 基底路径 / 补丁文件
  loom check                  复核所有锚点在当前经线里还找不找得到、唯不唯一
  loom anchors                列出每个锚点此刻解析到经线的哪一行
  loom view                   生成带锚点标注的派生视图

设置读的是最近的 loom.lm（从当前目录一路向上找）。
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2:]); err != nil {
		fmt.Fprintf(os.Stderr, "⛔ %v\n", err)
		os.Exit(1)
	}
}

func run(cmd string, args []string) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	cfgPath, err := loom.FindConfig(wd)
	if err != nil {
		return err
	}
	c, err := loom.LoadConfig(cfgPath)
	if err != nil {
		return err
	}

	switch cmd {
	case "weave":
		if len(args) != 1 {
			return fmt.Errorf("用法：loom weave <模板.lm>")
		}
		out, err := weaveFile(c, args[0])
		if err != nil {
			return err
		}
		_, err = os.Stdout.WriteString(out)
		return err

	case "build":
		outDir := ""
		if len(args) == 1 {
			outDir = args[0]
		}
		if outDir == "" {
			return fmt.Errorf("用法：loom build <产物目录>")
		}
		tpls, err := loom.Templates(c)
		if err != nil {
			return err
		}
		for _, p := range tpls {
			out, err := weaveFile(c, p)
			if err != nil {
				return err
			}
			t, err := loom.LoadTemplate(p)
			if err != nil {
				return err
			}
			dst := filepath.Join(outDir, t.Target)
			if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(dst, []byte(out), 0o644); err != nil {
				return err
			}
		}
		fmt.Printf("✓ 织了 %d 份 → %s\n", len(tpls), outDir)
		return nil

	case "list":
		if len(args) == 1 && args[0] == "-tsv" {
			return loom.ListTSV(c, os.Stdout)
		}
		return loom.List(c, os.Stdout)
	case "check":
		return loom.CheckAnchors(c, os.Stdout)
	case "anchors":
		return loom.ListAnchors(c, os.Stdout)
	case "view":
		return loom.AnchoredView(c, os.Stdout)
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	}
	return fmt.Errorf("未知子命令 `%s`\n%s", cmd, usage)
}

func weaveFile(c *loom.Config, path string) (string, error) {
	t, err := loom.LoadTemplate(path)
	if err != nil {
		return "", err
	}
	return loom.Weave(c, t)
}
