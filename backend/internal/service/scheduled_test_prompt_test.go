package service

import "testing"

func TestDefaultPelicanTestPrompt(t *testing.T) {
	prompt := DefaultPelicanTestPrompt
	want := "创建一个HTML，内容是SVG绘制一个鹈鹕骑自行车的2D动画，你不需要任何测试。"
	if prompt != want {
		t.Fatalf("default Pelican prompt = %q, want %q", prompt, want)
	}
}
