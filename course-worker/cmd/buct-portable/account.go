package main

import (
	"strings"
	"time"

	"buct-course-local/worker/internal/course"
	"buct-course-local/worker/internal/portable"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func (c *companion) buildAccount() fyne.CanvasObject {
	notices := widget.NewCheck("截止提醒（24、12、1 小时）", func(on bool) {
		value := int64(0)
		if on {
			value = 1
		}
		if err := c.store.SetSetting("notifications", value); err != nil {
			c.setStatus("无法保存提醒设置。")
		}
		c.checkNotices()
	})
	notices.Checked = c.store.Setting("notifications", 1) == 1
	return scroll(widget.NewCard("学校账号", "", container.NewVBox(c.accountStatus,
		widget.NewButton("连接学校账号", func() { c.login(nil) }),
		widget.NewButton("解锁已保存凭证", func() { c.unlock(nil) }),
		widget.NewButton("退出学校账号", c.signOut))),
		widget.NewCard("提醒与数据", "", container.NewVBox(notices,
			widget.NewButton("发送测试通知", func() {
				if !notificationsEnabled() {
					if !openNotificationSettings() {
						showError(c.win, "请在系统设置中允许北化课伴发送通知。")
					}
					return
				}
				c.app.SendNotification(fyne.NewNotification(appName, "这是一条测试通知。"))
			}),
			widget.NewButton("隐藏与回收站", c.openTrash))),
		wrap("学校页面在系统浏览器中打开。课程同步使用应用内的账号连接。"))
}

func (c *companion) updateAccount() {
	if c.account.StudentID != "" {
		c.accountStatus.SetText("学校账号已连接。")
	} else if data, err := c.store.Vault(); err == nil && len(data) > 0 {
		c.accountStatus.SetText("已有加密凭证，解锁后可同步。")
	} else {
		c.accountStatus.SetText("尚未连接学校账号。")
	}
}

func (c *companion) withAccount(then func()) {
	if c.account.StudentID != "" {
		then()
		return
	}
	if data, err := c.store.Vault(); err == nil && len(data) > 0 {
		c.unlock(then)
		return
	}
	c.login(then)
}

func (c *companion) login(then func()) {
	if c.busy {
		c.setStatus("请等待同步完成。")
		return
	}
	id := widget.NewEntry()
	id.SetText(c.account.StudentID)
	password := widget.NewPasswordEntry()
	vault := widget.NewPasswordEntry()
	vault.SetPlaceHolder("留空仅在此次运行中使用")
	c.showForm("连接学校账号", []*widget.FormItem{widget.NewFormItem("学号", id), widget.NewFormItem("学校密码", password),
		widget.NewFormItem("凭证保护密码", vault)}, func() {
		account := portable.Account{StudentID: strings.TrimSpace(id.Text), Password: password.Text}
		phrase := vault.Text
		password.SetText("")
		vault.SetText("")
		if account.StudentID == "" || account.Password == "" {
			showError(c.win, "请填写学号和学校密码。")
			return
		}
		var sealed []byte
		if phrase != "" {
			var err error
			sealed, err = portable.SealAccount(account, phrase)
			if err != nil {
				showError(c.win, err.Error())
				return
			}
		}
		c.run(course.Request{Mode: "login"}, account, "正在验证学校账号…", func(result course.Result) {
			if errorText := safeError(result); errorText != "" {
				c.setStatus(errorText)
				return
			}
			if err := c.store.SaveAccount(account.StudentID, sealed); err != nil {
				c.cookies = nil
				c.setStatus("无法保存凭证设置。")
				return
			}
			c.account = account
			c.renderTasks()
			c.renderCourses()
			c.updateAccount()
			c.setStatus("学校账号已连接。")
			c.nextSync = time.Now()
			if then != nil {
				then()
			} else {
				c.syncTasks()
			}
		})
	})
}

func (c *companion) unlock(then func()) {
	data, err := c.store.Vault()
	if err != nil || len(data) == 0 {
		showError(c.win, "没有可解锁的凭证。")
		return
	}
	entry := widget.NewPasswordEntry()
	c.showForm("解锁凭证", []*widget.FormItem{widget.NewFormItem("凭证保护密码", entry)}, func() {
		account, err := portable.OpenAccount(data, entry.Text)
		entry.SetText("")
		if err != nil {
			showError(c.win, err.Error())
			return
		}
		c.account = account
		c.cookies = nil
		c.updateAccount()
		c.setStatus("凭证已解锁。")
		if then != nil {
			then()
		} else {
			c.syncTasks()
		}
	})
}

func (c *companion) signOut() {
	if c.busy {
		c.setStatus("请等待同步完成。")
		return
	}
	dialog.ShowConfirm("退出学校账号", "清除学校凭证和课程缓存，个人待办和课表会保留。", func(ok bool) {
		if !ok {
			return
		}
		if err := c.store.SignOut(); err != nil {
			c.setStatus("退出失败，请重试。")
			return
		}
		c.account = portable.Account{}
		c.cookies = nil
		c.updateAccount()
		c.renderTasks()
		c.renderCourses()
		c.checkNotices()
		c.setStatus("已退出学校账号。")
	}, c.win)
}
