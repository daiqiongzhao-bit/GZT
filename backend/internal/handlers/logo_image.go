package handlers

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"

	// 注册 JPEG / PNG 解码器（ICO 容器里可能内嵌 PNG）
	_ "image/jpeg"
	_ "image/png"
)

// ===================== ICO 解码 =====================
// Go 标准库不支持 ICO，这里实现轻量解析：
//  1. ICONDIR(6B) + ICONDIRENTRY(16B × n)
//  2. 每个条目指向一段图像数据：可能是完整 PNG，也可能是 BITMAPINFOHEADER + DIB 位图 + AND 掩码
// 取尺寸最大（同尺寸时位深最高）的条目解码。

func decodeICO(data []byte) (image.Image, error) {
	if len(data) < 22 {
		return nil, errors.New("ico 文件过小")
	}
	if binary.LittleEndian.Uint16(data[0:2]) != 0 || binary.LittleEndian.Uint16(data[2:4]) != 1 {
		return nil, errors.New("不是有效的 ico 文件")
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if count <= 0 {
		return nil, errors.New("ico 中不含图像")
	}

	bestIdx, bestArea, bestBpp := -1, 0, 0
	var bestW, bestH, bestOff, bestSize int
	for i := 0; i < count; i++ {
		off := 6 + i*16
		if off+16 > len(data) {
			break
		}
		w := int(data[off])
		if w == 0 {
			w = 256
		}
		h := int(data[off+1])
		if h == 0 {
			h = 256
		}
		bpp := int(binary.LittleEndian.Uint16(data[off+6 : off+8]))
		size := int(binary.LittleEndian.Uint32(data[off+8 : off+12]))
		imgOff := int(binary.LittleEndian.Uint32(data[off+12 : off+16]))
		if size <= 0 || imgOff <= 0 || imgOff+size > len(data) {
			continue
		}
		area := w * h
		if area > bestArea || (area == bestArea && bpp > bestBpp) {
			bestIdx, bestArea, bestBpp = i, area, bpp
			bestW, bestH, bestOff, bestSize = w, h, imgOff, size
		}
	}
	if bestIdx < 0 {
		return nil, errors.New("ico 中无可用的图像数据")
	}

	chunk := data[bestOff : bestOff+bestSize]
	// 现代 ico 常直接内嵌 PNG
	if len(chunk) > 8 && bytes.HasPrefix(chunk, []byte("\x89PNG\r\n\x1a\n")) {
		return png.Decode(bytes.NewReader(chunk))
	}
	return decodeDIB(chunk, bestW, bestH)
}

// decodeDIB 解析 BITMAPINFOHEADER + XOR 位图 + AND 掩码
func decodeDIB(b []byte, w, h int) (image.Image, error) {
	if len(b) < 40 {
		return nil, errors.New("ico 位图头损坏")
	}
	bihSize := int(binary.LittleEndian.Uint32(b[0:4]))
	if bihSize < 40 || bihSize > len(b) {
		bihSize = 40
	}
	biBitCount := int(binary.LittleEndian.Uint16(b[14:16]))
	if binary.LittleEndian.Uint32(b[16:20]) != 0 {
		return nil, errors.New("不支持压缩格式的 ico，请改用 PNG 上传")
	}
	if w <= 0 || h <= 0 || w > 1024 || h > 1024 {
		return nil, errors.New("ico 尺寸异常")
	}
	src := b[bihSize:]
	m := image.NewNRGBA(image.Rect(0, 0, w, h))

	switch biBitCount {
	case 32:
		rowSize := ((w*32 + 31) / 32) * 4
		if len(src) < rowSize*h {
			return nil, errors.New("ico 位图数据不完整")
		}
		for y := 0; y < h; y++ {
			row := src[(h-1-y)*rowSize:] // DIB 行序为自下而上
			for x := 0; x < w; x++ {
				i := x * 4
				m.SetNRGBA(x, y, color.NRGBA{R: row[i+2], G: row[i+1], B: row[i], A: row[i+3]})
			}
		}
		applyANDMask(m, src[rowSize*h:], w, h)
		return m, nil
	case 24:
		rowSize := ((w*24 + 31) / 32) * 4
		if len(src) < rowSize*h {
			return nil, errors.New("ico 位图数据不完整")
		}
		for y := 0; y < h; y++ {
			row := src[(h-1-y)*rowSize:]
			for x := 0; x < w; x++ {
				i := x * 3
				m.SetNRGBA(x, y, color.NRGBA{R: row[i+2], G: row[i+1], B: row[i], A: 255})
			}
		}
		applyANDMask(m, src[rowSize*h:], w, h)
		return m, nil
	case 8, 4, 1:
		nColors := 1 << uint(biBitCount)
		if bihSize+4*nColors > len(b) {
			return nil, errors.New("ico 调色板损坏")
		}
		pal := make([]color.NRGBA, nColors)
		for i := 0; i < nColors; i++ {
			o := bihSize + i*4
			pal[i] = color.NRGBA{R: b[o+2], G: b[o+1], B: b[o], A: 255}
		}
		pix := b[bihSize+4*nColors:]
		rowSize := ((w*biBitCount + 31) / 32) * 4
		if len(pix) < rowSize*h {
			return nil, errors.New("ico 位图数据不完整")
		}
		for y := 0; y < h; y++ {
			row := pix[(h-1-y)*rowSize:]
			for x := 0; x < w; x++ {
				var idx int
				switch biBitCount {
				case 8:
					idx = int(row[x])
				case 4:
					if x%2 == 0 {
						idx = int(row[x/2] >> 4)
					} else {
						idx = int(row[x/2] & 0x0f)
					}
				case 1:
					idx = int((row[x/8] >> (7 - uint(x%8))) & 1)
				}
				if idx >= nColors {
					idx = 0
				}
				m.SetNRGBA(x, y, pal[idx])
			}
		}
		applyANDMask(m, pix[rowSize*h:], w, h)
		return m, nil
	}
	return nil, errors.New("不支持的 ico 位深，请改用 PNG 上传")
}

// applyANDMask 应用 1bpp AND 掩码：位为 1 表示该像素透明
func applyANDMask(m *image.NRGBA, mask []byte, w, h int) {
	if len(mask) == 0 {
		return
	}
	rowSize := ((w + 31) / 32) * 4
	if len(mask) < rowSize*h {
		return // 掩码缺失则整体不透明
	}
	for y := 0; y < h; y++ {
		row := mask[(h-1-y)*rowSize:]
		for x := 0; x < w; x++ {
			if (row[x/8]>>(7-uint(x%8)))&1 == 1 {
				i := m.PixOffset(x, y)
				m.Pix[i+3] = 0
			}
		}
	}
}

// ===================== 背景透明化 =====================

// removeBackground 把纯色（常见为白色）背景抠成透明。
// 采用「边缘连通」策略：仅从四边向内泛洪扩散，避免误伤 Logo 内部的白色元素。
func removeBackground(src image.Image) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return src
	}
	m := image.NewNRGBA(b)
	draw.Draw(m, b, src, b.Min, draw.Src)

	bg := edgeBackgroundColor(m)
	// 与背景色差异在此阈值内视为背景；再留一段渐变带做抗锯齿
	const hardT, softT int32 = 96, 108
	soft := func(x, y int) float64 {
		i := m.PixOffset(x, y)
		d := abs32(int32(m.Pix[i])-bg[0]) + abs32(int32(m.Pix[i+1])-bg[1]) + abs32(int32(m.Pix[i+2])-bg[2])
		if d <= hardT {
			return 0
		}
		if d >= hardT+softT {
			return 1
		}
		return float64(d-hardT) / float64(softT)
	}

	seen := make([]bool, w*h)
	queue := make([]int, 0, w*h)
	// 种子：四条边上"接近背景色"的像素
	seed := func(x, y int) {
		idx := y*w + x
		if seen[idx] || soft(x, y) >= 1 {
			return
		}
		seen[idx] = true
		queue = append(queue, idx)
	}
	for x := 0; x < w; x++ {
		seed(x, 0)
		if h > 1 {
			seed(x, h-1)
		}
	}
	for y := 0; y < h; y++ {
		seed(0, y)
		if w > 1 {
			seed(w-1, y)
		}
	}

	for head := 0; head < len(queue); head++ {
		idx := queue[head]
		x, y := idx%w, idx/w
		i := m.PixOffset(x, y)
		a := soft(x, y)
		// 与已有 alpha 取较小值：保留原图自带的透明通道
		orig := float64(m.Pix[i+3]) / 255
		if a < orig {
			orig = a
		}
		m.Pix[i+3] = uint8(orig * 255)
		// 四邻扩散
		for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			nx, ny := x+d[0], y+d[1]
			if nx < 0 || ny < 0 || nx >= w || ny >= h {
				continue
			}
			nidx := ny*w + nx
			if seen[nidx] || soft(nx, ny) >= 1 {
				continue
			}
			seen[nidx] = true
			queue = append(queue, nidx)
		}
	}
	return m
}

// edgeBackgroundColor 统计边框一圈像素的颜色直方图，取出现最多的量化色作为背景基准
func edgeBackgroundColor(m *image.NRGBA) [3]int32 {
	b := m.Bounds()
	w, h := b.Dx(), b.Dy()
	hist := map[[3]int32]int{}
	bestKey := [3]int32{}
	bestN := -1
	add := func(x, y int) {
		i := m.PixOffset(x, y)
		if m.Pix[i+3] < 8 {
			return // 已透明的边缘像素不参与统计
		}
		// 量化到 16 级，容忍轻微噪点
		k := [3]int32{int32(m.Pix[i]) / 16, int32(m.Pix[i+1]) / 16, int32(m.Pix[i+2]) / 16}
		hist[k]++
		if hist[k] > bestN {
			bestN = hist[k]
			bestKey = k
		}
	}
	for x := 0; x < w; x++ {
		add(x, 0)
		if h > 1 {
			add(x, h-1)
		}
	}
	for y := 0; y < h; y++ {
		add(0, y)
		if w > 1 {
			add(w-1, y)
		}
	}
	// 求该簇内像素的平均色，作为最终基准
	var sum [3]int32
	n := 0
	for x := 0; x < w; x++ {
		for _, y := range []int{0, h - 1} {
			if y < 0 {
				continue
			}
			i := m.PixOffset(x, y)
			if m.Pix[i+3] < 8 {
				continue
			}
			k := [3]int32{int32(m.Pix[i]) / 16, int32(m.Pix[i+1]) / 16, int32(m.Pix[i+2]) / 16}
			if k == bestKey {
				sum[0] += int32(m.Pix[i])
				sum[1] += int32(m.Pix[i+1])
				sum[2] += int32(m.Pix[i+2])
				n++
			}
		}
	}
	for y := 1; y < h-1; y++ {
		for _, x := range []int{0, w - 1} {
			if x < 0 {
				continue
			}
			i := m.PixOffset(x, y)
			if m.Pix[i+3] < 8 {
				continue
			}
			k := [3]int32{int32(m.Pix[i]) / 16, int32(m.Pix[i+1]) / 16, int32(m.Pix[i+2]) / 16}
			if k == bestKey {
				sum[0] += int32(m.Pix[i])
				sum[1] += int32(m.Pix[i+1])
				sum[2] += int32(m.Pix[i+2])
				n++
			}
		}
	}
	if n == 0 {
		return [3]int32{255, 255, 255} // 全透明边：按白底处理
	}
	return [3]int32{sum[0] / int32(n), sum[1] / int32(n), sum[2] / int32(n)}
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// fitSize 等比缩放到不超过 max 边长
func fitSize(src image.Image, max int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= max && h <= max {
		return src
	}
	var nw, nh int
	if w >= h {
		nw, nh = max, h*max/w
	} else {
		nh, nw = max, w*max/h
	}
	if nw < 1 {
		nw = 1
	}
	if nh < 1 {
		nh = 1
	}
	dst := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	// 双线性插值（标准库 image/draw 无插值器，这里按 alpha 预乘混合，避免透明边缘出现黑边）
	rx := float64(w) / float64(nw)
	ry := float64(h) / float64(nh)
	for y := 0; y < nh; y++ {
		fy := (float64(y)+0.5)*ry - 0.5
		y0 := int(math.Floor(fy))
		wy := fy - float64(y0)
		if y0 < 0 {
			y0, wy = 0, 0
		}
		y1 := y0 + 1
		if y1 >= h {
			y1 = h - 1
		}
		for x := 0; x < nw; x++ {
			fx := (float64(x)+0.5)*rx - 0.5
			x0 := int(math.Floor(fx))
			wx := fx - float64(x0)
			if x0 < 0 {
				x0, wx = 0, 0
			}
			x1 := x0 + 1
			if x1 >= w {
				x1 = w - 1
			}
			a := premul(src.At(b.Min.X+x0, b.Min.Y+y0))
			bb := premul(src.At(b.Min.X+x1, b.Min.Y+y0))
			cc := premul(src.At(b.Min.X+x0, b.Min.Y+y1))
			dd := premul(src.At(b.Min.X+x1, b.Min.Y+y1))
			var out [4]float64
			for k := 0; k < 4; k++ {
				top := a[k]*(1-wx) + bb[k]*wx
				bot := cc[k]*(1-wx) + dd[k]*wx
				out[k] = top*(1-wy) + bot*wy
			}
			dst.SetNRGBA(x, y, unpremul(out))
		}
	}
	return dst
}

// premul 把颜色转为 [0,1] 的 alpha 预乘值
func premul(c color.Color) [4]float64 {
	r, g, bb, a := c.RGBA()
	af := float64(a) / 65535
	return [4]float64{float64(r) / 65535 * af, float64(g) / 65535 * af, float64(bb) / 65535 * af, af}
}

func unpremul(v [4]float64) color.NRGBA {
	a := v[3]
	if a <= 0 {
		return color.NRGBA{}
	}
	to := func(x float64) uint8 {
		n := x / a * 255
		if n < 0 {
			n = 0
		}
		if n > 255 {
			n = 255
		}
		return uint8(n + 0.5)
	}
	return color.NRGBA{R: to(v[0]), G: to(v[1]), B: to(v[2]), A: uint8(a*255 + 0.5)}
}
