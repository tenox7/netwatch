/*
 * Copyright 2019 Google LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package main

import (
	"container/ring"
	_ "embed"
	"flag"
	"fmt"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/veandco/go-sdl2/sdl"
	"github.com/veandco/go-sdl2/ttf"
	"golang.org/x/exp/maps"
)

type probe func(string, chan []float64) error

var (
	pingInterval       = 1 * time.Second
	pingTimeout        = 4 * time.Second
	size         uint  = 56
	title              = "netwatch"
	windowWidth  int32 = 180
	windowHeight int32 = 100
	windowMargin int32 = 5
	panelHeight  int32 = 45
	targetSize   int32 = 90
	panelWidth         = windowWidth - (windowMargin * 2)
	fontName           = "noto.ttf"
	fontSize     int32 = 11
	fg                 = sdl.Color{0xFF, 0xFF, 0xFF, 0xFF}
	bg                 = sdl.Color{0x64, 0x64, 0x64, 0xFF}
	errColor           = sdl.Color{0xFF, 0x64, 0x64, 0xFF}
	plotColors         = []sdl.Color{
		{0x00, 0xFF, 0x00, 0xFF},
		{0x00, 0x00, 0xFF, 0xFF},
		{0xFF, 0x7F, 0x00, 0xFF},
	}

	//go:embed fonts/noto.ttf
	fontData []byte

	// for loops without a ticker use more CPU than for loops with a ticker,
	// even if channel writes are blocking and the channel is only read on the
	// update interval.
	probes = map[string]probe{
		// Basic test probe
		"sine": func(target string, c chan []float64) error {
			go func() {
				i := 0.0
				for range time.Tick(pingInterval) {
					c <- []float64{math.Sin(i) + 1}
					i += 0.1
				}
			}()
			return nil
		},
		// Timeout test probe
		"lagsine": func(target string, c chan []float64) error {
			go func() {
				i := 0.0
				for range time.Tick(pingInterval * 5) {
					c <- []float64{math.Sin(i) + 1}
					i += 0.1
				}
			}()
			return nil
		},
		// Multi-plot test probe
		"multi": func(target string, c chan []float64) error {
			go func() {
				i := 0.0
				for range time.Tick(pingInterval) {
					c <- []float64{math.Sin(i) + 1, math.Cos(i) + 1, rand.Float64() * 2}
					i += 0.1
				}
			}()
			return nil
		},
	}
)

func errbox(format string, args ...interface{}) {
	sdl.Init(sdl.INIT_VIDEO)
	window, _ := sdl.CreateWindow(title, sdl.WINDOWPOS_UNDEFINED, sdl.WINDOWPOS_UNDEFINED, 100, 100, sdl.WINDOW_HIDDEN)
	sdl.ShowSimpleMessageBox(10, "Error", fmt.Sprintf(format, args...), window)
	window.Destroy()
	os.Exit(1)
}

func drawText(renderer *sdl.Renderer, font *ttf.Font, color sdl.Color, x int32, y int32, text string) (int32, int32) {
	var surface *sdl.Surface
	var texture *sdl.Texture

	surface, _ = font.RenderUTF8Blended(text, color)
	texture, _ = renderer.CreateTextureFromSurface(surface)
	_, _, w, h, _ := texture.Query()
	renderer.Copy(texture, nil, &sdl.Rect{x, y, w, h})
	surface.Free()
	texture.Destroy()
	return w, h
}

func plotRing(r *ring.Ring, host string, tgtnum int32, renderer *sdl.Renderer, font *ttf.Font, fg sdl.Color) {
	var minv, maxv, avg, lst, tot float64 = 10000.0, 0, 0, 0, 0
	var i, h int32 = 0, 0
	var txt string
	var vs int32 = (tgtnum * targetSize) + 1
	var data, prev []float64

	renderer.SetDrawColor(fg.R, fg.G, fg.B, fg.A)
	renderer.DrawRect(&sdl.Rect{windowMargin, vs + windowMargin + 15, windowWidth - (windowMargin * 2), panelHeight})
	drawText(renderer, font, fg, windowMargin, vs+windowMargin-3, host)

	r.Do(func(x interface{}) {
		data, _ = x.([]float64)
		for _, v := range data {
			lst = v
			if v > maxv {
				maxv = v
			}
			if v < minv {
				minv = v
			}
			if v > 0 {
				tot += v
				i++
			}
		}
	})

	avg = tot / float64(i)
	i = 0

	vh := func(v float64) int32 {
		return int32((v/maxv)*float64(panelHeight-2)) - 1
	}

	r.Do(func(x interface{}) {
		prev = data
		data, _ = x.([]float64)
		for j, v := range data {
			if math.IsNaN(v) {
				h = panelHeight - 1
				renderer.SetDrawColor(errColor.R, errColor.G, errColor.B, errColor.A)
			} else if v > 0 {
				h = vh(v)
				if len(plotColors) > j {
					renderer.SetDrawColor(plotColors[j].R, plotColors[j].G, plotColors[j].B, plotColors[j].A)
				}
			} else {
				h = 2
				renderer.SetDrawColor(0x00, 0xFF, 0x00, 0xFF)
			}
			x, y1, y2 := windowMargin+i, vs+windowMargin+panelHeight+13, vs+windowMargin+panelHeight+13
			if j > 0 && len(prev) > j {
				y1 -= min(vh(v), vh(prev[j]))
				y2 -= max(vh(v), vh(prev[j]))
			} else {
				y1 -= h
			}
			renderer.DrawLine(x, y1, x, y2)
		}
		i++
	})
	txt = fmt.Sprintf("L=%.1f M=%.1f A=%.1f", lst, maxv, avg)
	drawText(renderer, font, fg, windowMargin, vs+windowMargin+panelHeight+15, txt)
}

type panel struct {
	typ, target string
	channel     chan []float64
	ring        *ring.Ring
}

func parsePanels(args []string) ([]*panel, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("No args specified")
	} else if len(args) > int(^byte(0)) {
		return nil, fmt.Errorf("Too many targets specified")
	}

	a := make([]*panel, len(args))
	for i, arg := range args {
		tokens := strings.Split(arg, ":")
		if len(tokens) != 2 {
			return nil, fmt.Errorf("Could not parse panel: %q", arg)
		}

		p, ok := probes[tokens[0]]
		if !ok {
			return nil, fmt.Errorf("Unsupported panel type: %q", tokens[0])
		}

		c := make(chan []float64)
		err := p(tokens[1], c)
		if err != nil {
			return nil, fmt.Errorf("Error initializing %s: %w", tokens[0], err)
		}

		a[i] = &panel{
			typ:     tokens[0],
			target:  tokens[1],
			channel: c,
			ring:    ring.New(int(panelWidth - 2)),
		}
	}

	return a, nil
}

func main() {
	// Parse CLI args

	var foreground bool
	var fullScreen bool
	var bgColor string
	var fgColor string

	flag.Usage = func() {
		errbox("Usage:\n%s type:target [type:target [...]]\n\nPanel types are:\n%s",
			os.Args[0], strings.Join(maps.Keys(probes), ", "))
	}

	flag.BoolVar(&fullScreen, "fs", false, "Run in full screen mode")
	flag.StringVar(&bgColor, "bg", "", "Background Color in Hex RRGGBB")
	flag.StringVar(&fgColor, "fg", "", "Border and Text Color in Hex RRGGBB")
	flag.BoolVar(&foreground, "f", false, "Run in foreground, do not detach from terminal")
	flag.Parse()

	if !foreground {
		cwd, err := os.Getwd()
		if err != nil {
			errbox("Getcwd error: %s\n", err)
		}
		args := []string{"-f"}
		args = append(args, os.Args[1:]...)
		cmd := exec.Command(os.Args[0], args...)
		cmd.Dir = cwd
		if err := cmd.Start(); err != nil {
			errbox("Startup error: %s\n", err)
		}
		cmd.Process.Release()
		os.Exit(0)
	}

	panels, err := parsePanels(flag.Args())
	if err != nil {
		errbox(err.Error())
	}

	if len(bgColor) == 6 {
		n, err := fmt.Sscanf(bgColor, "%2x%2x%2x", &bg.R, &bg.G, &bg.B)
		if err != nil || n != 3 {
			errbox("Unable to parse bg background color")
		}
	}

	if len(fgColor) == 6 {
		n, err := fmt.Sscanf(fgColor, "%2x%2x%2x", &fg.R, &fg.G, &fg.B)
		if err != nil || n != 3 {
			errbox("Unable to parse fg foreground color")
		}
	}

	// SDL Init

	if err := sdl.Init(sdl.INIT_VIDEO); err != nil {
		errbox("Failed to initialize SDL:\n %s\n", err)
	}

	if err := ttf.Init(); err != nil {
		errbox("Failed to initialize TTF:\n %s\n", err)
	}

	window, err := sdl.CreateWindow(title, sdl.WINDOWPOS_UNDEFINED, sdl.WINDOWPOS_UNDEFINED, windowWidth, int32(len(panels))*targetSize, sdl.WINDOW_OPENGL)
	if err != nil {
		errbox("Failed to create window:\n %s\n", err)
	}
	defer window.Destroy()

	if fullScreen {
		window.SetFullscreen(sdl.WINDOW_FULLSCREEN_DESKTOP)
	}

	windowWidth, windowHeight = window.GetSize()
	panelWidth = windowWidth - (windowMargin * 2)

	renderer, err := sdl.CreateRenderer(window, -1, sdl.RENDERER_ACCELERATED)
	if err != nil {
		errbox("Failed to create renderer:\n %s\n", err)
	}
	defer renderer.Destroy()

	// Font init

	tmp, err := os.CreateTemp(os.TempDir(), fontName)
	if err != nil {
		errbox("Unable to create temp file:\n%s\n", err)
	}
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(fontData); err != nil {
		errbox("Unable to write temp font:\n%s\n", err)
	}
	tmp.Close()

	font, err := ttf.OpenFont(tmp.Name(), int(fontSize))
	if err != nil {
		errbox("Failed to open font:\n %s\n", err)
	}

	// Render Loop
	go func() {
		for range time.Tick(pingInterval) {
			renderer.SetDrawColor(bg.R, bg.G, bg.B, 0xFF)
			renderer.Clear()

			for i, panel := range panels {
				var color sdl.Color

				select {
				case v := <-panel.channel:
					panel.ring.Value = v
					panel.ring = panel.ring.Next()
					color = fg
				default:
					color = errColor
				}

				plotRing(panel.ring, panel.target, int32(i), renderer, font, color)
			}

			renderer.Present()
		}
	}()

	// Event Loop
	for event := sdl.WaitEvent(); event != nil; event = sdl.WaitEvent() {
		switch event.(type) {
		case *sdl.QuitEvent:
			os.Exit(0)
		}
	}
}
