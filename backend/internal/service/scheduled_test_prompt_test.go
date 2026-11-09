package service

import (
	"strings"
	"testing"
)

func TestDefaultPelicanTestPrompt(t *testing.T) {
	prompt := DefaultPelicanTestPrompt
	for _, required := range []string{
		PreviousDefaultPelicanTestPrompt,
		"每次测智都是一次新会话",
		"输出可保存的完整HTML源码",
		"不要声称已经创建文件",
		"不运行测试",
	} {
		if !strings.Contains(prompt, required) {
			t.Errorf("default Pelican prompt %q does not contain %q", prompt, required)
		}
	}
}
