// 把对可执行文件的查找单独放在这里，方便测试替换。
package procproxy

import "os/exec"

func lookPath(name string) (string, error) {
	return exec.LookPath(name)
}
