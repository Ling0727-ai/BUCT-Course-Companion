package main

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"buct-course-local/worker/internal/portable"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

const appID = "io.github.ling0727ai.buctcoursecompanion"
const appName = "北化课伴"

//go:embed assets/Icon.png assets/NotoSansSC.ttf assets/OFL.txt
var bundled embed.FS

func asset(name string) fyne.Resource {
	content, err := bundled.ReadFile("assets/" + name)
	if err != nil {
		panic(err)
	}
	return fyne.NewStaticResource(name, content)
}

func main() {
	time.Local = time.FixedZone("Asia/Shanghai", 8*3600)
	a := app.NewWithID(appID)
	a.SetIcon(asset("Icon.png"))
	a.Settings().SetTheme(frostedTheme(asset("NotoSansSC.ttf")))
	w := a.NewWindow(appName)
	isMobile := a.Driver().Device().IsMobile()
	if !isMobile {
		w.Resize(fyne.NewSize(1100, 730))
	}
	root := a.Storage().RootURI().Path()
	if err := os.MkdirAll(root, 0700); err != nil {
		w.SetContent(widget.NewLabel("无法创建本地数据目录。"))
		w.ShowAndRun()
		return
	}
	store, err := portable.Open(filepath.Join(root, "course.db"))
	if err != nil {
		w.SetContent(widget.NewLabel("无法打开本地课伴数据库。"))
		w.ShowAndRun()
		return
	}
	defer store.Close()
	c := newCompanion(a, w, store, isMobile)
	w.SetContent(c.content())
	if os.Getenv("BUCT_PORTABLE_SMOKE") == "1" && !isMobile {
		w.Show()
		go func() {
			time.Sleep(500 * time.Millisecond)
			fyne.Do(func() { fmt.Println("BUCT portable app opened"); a.Quit() })
		}()
		a.Run()
		return
	}
	if tray, ok := a.(desktop.App); ok {
		tray.SetSystemTrayIcon(asset("Icon.png"))
		tray.SetSystemTrayWindow(w)
		tray.SetSystemTrayMenu(fyne.NewMenu(appName,
			fyne.NewMenuItem("打开课伴", w.Show),
			fyne.NewMenuItem("同步课程", c.syncTasks),
			fyne.NewMenuItem("退出", a.Quit)))
		w.SetCloseIntercept(w.Hide)
	}
	w.SetOnClosed(c.stop)
	w.Canvas().SetOnTypedKey(func(event *fyne.KeyEvent) {
		if event.Name == fyne.KeyEscape && !isMobile {
			w.Canvas().Unfocus()
		}
	})
	c.start()
	defer c.stop()
	w.ShowAndRun()
}

func showError(w fyne.Window, message string) {
	dialog.ShowInformation("北化课伴", message, w)
}
