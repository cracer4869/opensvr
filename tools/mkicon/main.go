// Command mkicon 生成 opensvr 的应用/托盘图标：把「蓝色渐变圆角底 + 白色地球」
// 直接用纯 Go 光栅化为多尺寸 PNG，并打包成一个多尺寸 ICO 文件。
//
// 用法（在本目录内执行，因其为独立嵌套模块，不属于主模块 ./...）：
//
//	go run .
//
// 产物：
//   - <项目根>/cmd/opensvr/icon.ico   多尺寸 ICO（16/24/32/48/64/128/256）
//   - <项目根>/_icon_preview.png      256 尺寸预览图，供人工核对
//
// 设计说明：曾尝试用 oksvg 光栅化 web 端 favicon.svg，但 oksvg 只渲染出渐变
// 矩形底，丢失了 <g fill> 组内的白色地球路径与 rect 的圆角(rx)。因此改为直接
// 绘制：几何上重建「圆角渐变底 + 白色地球圆盘 + 蓝色经纬网格」，通过 4× 超采样
// + 盒式降采样得到抗锯齿边缘。仅依赖标准库，无 CGO、无第三方依赖。
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
)

// iconSizes 为 ICO 内包含的方形像素尺寸。
var iconSizes = []int{16, 24, 32, 48, 64, 128, 256}

// 配色沿用 web 端 favicon.svg。
var (
	topBlue  = rgb{0x4d, 0xa3, 0xff} // 渐变顶部
	botBlue  = rgb{0x2b, 0x6c, 0xb0} // 渐变底部
	gridBlue = rgb{0x2b, 0x6c, 0xb0} // 白色圆盘上的经纬线
	white    = rgb{0xff, 0xff, 0xff}
)

type rgb struct{ r, g, b float64 }

func lerp(a, b rgb, t float64) rgb {
	return rgb{a.r + (b.r-a.r)*t, a.g + (b.g-a.g)*t, a.b + (b.b-a.b)*t}
}

func main() {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		fatal(fmt.Errorf("无法定位源文件路径"))
	}
	projRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	icoPath := filepath.Join(projRoot, "cmd", "opensvr", "icon.ico")
	previewPath := filepath.Join(projRoot, "_icon_preview.png")

	imgs := make([]*image.RGBA, 0, len(iconSizes))
	for _, px := range iconSizes {
		img := drawIcon(px)
		imgs = append(imgs, img)
		// 可选：导出各尺寸单图便于人工核对（设置 MKICON_DUMP=目录 时生效）。
		if dir := os.Getenv("MKICON_DUMP"); dir != "" {
			_ = writePNG(filepath.Join(dir, fmt.Sprintf("icon_%d.png", px)), img)
		}
	}

	if err := writePNG(previewPath, imgs[len(imgs)-1]); err != nil {
		fatal(fmt.Errorf("写预览 PNG 失败: %w", err))
	}

	icoData, err := encodeICO(imgs)
	if err != nil {
		fatal(fmt.Errorf("编码 ICO 失败: %w", err))
	}
	if err := os.WriteFile(icoPath, icoData, 0o644); err != nil {
		fatal(fmt.Errorf("写 ICO 失败: %w", err))
	}

	got, err := verifyICO(icoData)
	if err != nil {
		fatal(fmt.Errorf("回读校验 ICO 失败: %w", err))
	}

	fmt.Printf("已写出 ICO: %s (%d 字节)\n", icoPath, len(icoData))
	fmt.Printf("已写出预览: %s\n", previewPath)
	fmt.Printf("ICO 尺寸: %v\n", got)
	if !contains(got, 16) || !contains(got, 256) {
		fatal(fmt.Errorf("ICO 缺少必需的 16 或 256 尺寸: %v", got))
	}
	fmt.Println("校验通过：包含 16 与 256 尺寸，且各子图可被 image/png 解码。")
}

// drawIcon 以 4× 超采样绘制 px×px 图标，再盒式降采样得到抗锯齿结果。
func drawIcon(px int) *image.RGBA {
	const ss = 4
	H := float64(px * ss)

	// 几何参数（均以高分辨率坐标计）。
	radius := 7.0 / 32.0 * H // 圆角半径，与 favicon rx=7(viewBox32) 同比例
	cx, cy := H/2, H/2       // 圆心
	R := 0.34 * H            // 地球圆盘半径
	// 经纬线半宽：换算到最终像素约 max(px*0.03, 1.1)/2，保证 16px 下仍清晰。
	hw := math.Max(float64(px)*0.03, 1.1) / 2 * ss

	out := image.NewRGBA(image.Rect(0, 0, px, px))
	n := ss * ss
	for oy := 0; oy < px; oy++ {
		for ox := 0; ox < px; ox++ {
			var sr, sg, sb, sa float64
			for dy := 0; dy < ss; dy++ {
				for dx := 0; dx < ss; dx++ {
					x := float64(ox*ss+dx) + 0.5
					y := float64(oy*ss+dy) + 0.5
					c, a := pixel(x, y, H, radius, cx, cy, R, hw)
					// 预乘 alpha 累加，避免圆角边缘出现暗边。
					sr += c.r * a
					sg += c.g * a
					sb += c.b * a
					sa += a
				}
			}
			i := out.PixOffset(ox, oy)
			out.Pix[i+0] = clamp8(sr / float64(n))
			out.Pix[i+1] = clamp8(sg / float64(n))
			out.Pix[i+2] = clamp8(sb / float64(n))
			out.Pix[i+3] = clamp8(sa / float64(n) * 255)
		}
	}
	return out
}

// pixel 返回高分辨率坐标 (x,y) 处的颜色与覆盖度 alpha(0..1)。
func pixel(x, y, H, radius, cx, cy, R, hw float64) (rgb, float64) {
	// 圆角矩形之外：全透明。
	if !insideRoundRect(x, y, H, radius) {
		return rgb{}, 0
	}
	// 底色：竖直渐变。
	col := lerp(topBlue, botBlue, y/H)

	// 地球圆盘。
	ex, ey := x-cx, y-cy
	if math.Hypot(ex, ey) <= R {
		col = white
		if onGrid(ex, ey, R, hw) {
			col = gridBlue
		}
	}
	return col, 1
}

// onGrid 判断 (ex,ey)（相对圆心）是否落在经纬网格线上：
// 中央经线(竖直) + 一对侧经线(椭圆) + 赤道(扁椭圆)。
func onGrid(ex, ey, R, hw float64) bool {
	if math.Abs(ex) <= hw { // 中央经线
		return true
	}
	if ellipseNear(ex, ey, 0.5*R, R, hw) { // 侧经线椭圆（外框给出左右两条经线）
		return true
	}
	if ellipseNear(ex, ey, R, 0.34*R, hw) { // 赤道（扁椭圆，front/back 两弧）
		return true
	}
	return false
}

// ellipseNear 近似判断点到中心椭圆(半轴 rx,ry)轮廓的距离是否 ≤ hw。
func ellipseNear(ex, ey, rx, ry, hw float64) bool {
	f := (ex*ex)/(rx*rx) + (ey*ey)/(ry*ry) - 1
	gx := 2 * ex / (rx * rx)
	gy := 2 * ey / (ry * ry)
	gm := math.Hypot(gx, gy)
	if gm == 0 {
		return false
	}
	return math.Abs(f)/gm <= hw
}

// insideRoundRect 判断 (x,y) 是否在 [0,H]² 的圆角矩形内（圆角半径 radius）。
func insideRoundRect(x, y, H, radius float64) bool {
	nx := math.Min(math.Max(x, radius), H-radius)
	ny := math.Min(math.Max(y, radius), H-radius)
	ddx, ddy := x-nx, y-ny
	return ddx*ddx+ddy*ddy <= radius*radius
}

// encodeICO 手写多尺寸 ICO 容器：ICONDIR + N×ICONDIRENTRY + 各尺寸 PNG 数据。
// 每个目录项直接指向一个完整 PNG（Vista+ 支持）；宽/高为 256 时字节填 0。
func encodeICO(imgs []*image.RGBA) ([]byte, error) {
	type entry struct {
		w, h int
		data []byte
	}
	entries := make([]entry, 0, len(imgs))
	for _, img := range imgs {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			return nil, err
		}
		b := img.Bounds()
		entries = append(entries, entry{w: b.Dx(), h: b.Dy(), data: buf.Bytes()})
	}

	var out bytes.Buffer
	// ICONDIR: reserved=0, type=1(图标), count
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))
	_ = binary.Write(&out, binary.LittleEndian, uint16(len(entries)))

	// 首个像素数据偏移 = 6(ICONDIR) + 16*N(ICONDIRENTRY)。
	offset := uint32(6 + 16*len(entries))
	for _, e := range entries {
		out.WriteByte(dim(e.w)) // 宽（256→0）
		out.WriteByte(dim(e.h)) // 高（256→0）
		out.WriteByte(0)        // 调色板颜色数（0=不使用）
		out.WriteByte(0)        // 保留
		_ = binary.Write(&out, binary.LittleEndian, uint16(1))            // 色彩平面
		_ = binary.Write(&out, binary.LittleEndian, uint16(32))           // 位深
		_ = binary.Write(&out, binary.LittleEndian, uint32(len(e.data)))  // 数据字节数
		_ = binary.Write(&out, binary.LittleEndian, offset)              // 数据偏移
		offset += uint32(len(e.data))
	}
	for _, e := range entries {
		out.Write(e.data)
	}
	return out.Bytes(), nil
}

// verifyICO 解析 ICO 目录并解码其中每个 PNG，返回各子图实际像素宽度。
func verifyICO(data []byte) ([]int, error) {
	if len(data) < 6 {
		return nil, fmt.Errorf("ICO 头部过短")
	}
	if binary.LittleEndian.Uint16(data[2:4]) != 1 {
		return nil, fmt.Errorf("非法 ICO 类型")
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if count == 0 {
		return nil, fmt.Errorf("ICO 不含任何子图")
	}
	sizes := make([]int, 0, count)
	for i := 0; i < count; i++ {
		base := 6 + 16*i
		if base+16 > len(data) {
			return nil, fmt.Errorf("目录项 %d 越界", i)
		}
		sz := binary.LittleEndian.Uint32(data[base+8 : base+12])
		off := binary.LittleEndian.Uint32(data[base+12 : base+16])
		if int(off)+int(sz) > len(data) {
			return nil, fmt.Errorf("子图 %d 数据越界", i)
		}
		img, err := png.Decode(bytes.NewReader(data[off : off+sz]))
		if err != nil {
			return nil, fmt.Errorf("子图 %d 解码失败: %w", i, err)
		}
		sizes = append(sizes, img.Bounds().Dx())
	}
	return sizes, nil
}

// dim 把像素尺寸编码为 ICONDIRENTRY 的单字节（256 及以上填 0）。
func dim(v int) byte {
	if v >= 256 {
		return 0
	}
	return byte(v)
}

func clamp8(v float64) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 255 {
		return 255
	}
	return uint8(v + 0.5)
}

func contains(xs []int, v int) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "mkicon:", err)
	os.Exit(1)
}
