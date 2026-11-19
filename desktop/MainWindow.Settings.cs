using Microsoft.Win32;
using Forms = System.Windows.Forms;

namespace BuctCourseLocal;

public partial class MainWindow
{
    private async void Settings_Click(object sender, RoutedEventArgs e)
    {
        var dialog = new EditDialog(this, "账户与通知");
        var choice = "";
        dialog.AddDescription(account is null ? "课程会话可以在内置页面登录，也可以保存学校凭证以便会话过期时自动登录。"
            : $"已保存学校账号 {account.StudentId}，凭证由当前 Windows 用户加密。更换账号请先退出。");
        void Option(string caption, string value, bool enabled = true)
        {
            var button = new Button { Content = caption, IsEnabled = enabled };
            button.Click += (_, _) => { choice = value; dialog.Close(); };
            dialog.AddOption(button);
        }
        Option(account is null ? "保存学号与密码" : "更新学校密码", "account", !busy);
        Option("在内置页面登录", "browser", !busy);
        Option("退出学校账号并清除课程缓存", "logout", !busy && (account is not null || savedCookies.Count > 0 || afterLogin is not null));
        Option("导入旧版 MongoDB 导出文件", "import", !busy);
        dialog.AddDescription("关闭窗口后应用会留在 Windows 托盘，继续发送截止提醒。彻底退出后提醒暂停。");
        var enabled = new CheckBox { Content = "启用截止前 24 / 12 / 1 小时通知", IsChecked = NoticesOn, Margin = new Thickness(0, 10, 0, 4) };
        dialog.AddField("Windows 通知", enabled);
        Option("发送测试通知", "test");
        dialog.AddAction("完成", dialog.Close, primary: true);
        ShowDialog(dialog);

        store.SetSetting("notifications", enabled.IsChecked == true ? 1 : 0);
        UpdateBoard();
        switch (choice)
        {
            case "account":
                if (AskCredentials() is { } credentials)
                    await SaveCredentialsAsync(credentials);
                break;
            case "browser":
                await RunAsync("正在打开学校登录…", async () =>
                {
                    await EnsureBrowserAsync();
                    BeginBrowserLogin(new Uri(CourseService.HomeUrl), true);
                    Status.Text = "请在内置页面完成学校认证。";
                });
                break;
            case "logout":
                if (MessageBox.Show(this, "清除已保存的学校凭证、会话和课程缓存？个人待办会继续保留。", "退出学校账号", MessageBoxButton.YesNo) == MessageBoxResult.Yes)
                    await ForgetAccountAsync();
                break;
            case "test":
                noticeOpensSchedule = false;
                noticeTarget = null;
                tray.ShowBalloonTip(7000, "北化课程提醒", "Windows 通知测试。点击返回任务列表。", Forms.ToolTipIcon.Info);
                Status.Text = "测试通知已提交给 Windows。";
                break;
            case "import":
                await ImportLegacyAsync();
                break;
        }
    }

    private Credentials? AskCredentials()
    {
        var dialog = new EditDialog(this, "学校登录凭证");
        dialog.AddDescription("保存前会验证学校账号。课程会话过期后，应用会用此凭证重新登录。");
        var id = new TextBox { Text = account?.StudentId ?? "", IsReadOnly = account is not null, MaxLength = 64 };
        var password = new PasswordBox { MaxLength = 256 };
        Credentials? submitted = null;
        dialog.AddField("学号", id);
        dialog.AddField("密码", password);
        dialog.AddAction("取消", dialog.Close, cancel: true);
        dialog.AddAction("验证并保存", () =>
        {
            if (string.IsNullOrWhiteSpace(id.Text) || password.Password.Length == 0)
            {
                dialog.ShowError("请输入学校学号和密码。");
                return;
            }
            submitted = new Credentials(id.Text.Trim(), password.Password);
            password.Clear();
            dialog.Close();
        }, primary: true);
        dialog.Loaded += (_, _) => { if (account is null) id.Focus(); else password.Focus(); };
        ShowDialog(dialog);
        password.Clear();
        return submitted;
    }

    private Task SaveCredentialsAsync(Credentials credentials) => RunAsync("正在验证学校账号…", async () =>
    {
        await EnsureBrowserAsync();
        var result = await RunCourseAsync("login", credentials, []);
        if (result.NeedsLogin || result.Error is not null)
        {
            Status.Text = result.Error ?? "验证失败，请在内置页面完成学校认证。";
            if (result.NeedsLogin)
                BeginBrowserLogin(new Uri(CourseService.HomeUrl), true);
            return;
        }
        await ClearBrowserSessionAsync();
        await ApplyCourseSessionAsync(result);
        store.SaveSecret("account", credentials);
        account = credentials;
        afterLogin = null;
        LoginBanner.Visibility = Visibility.Collapsed;
        UpdateBoard();
        Status.Text = "凭证已加密保存，正在同步课程…";
        nextSync = Now + 15 * 60;
        var sync = await RunCourseAsync("sync", null, await ReadCookiesAsync());
        if (sync.NeedsLogin)
        {
            Status.Text = "已保存凭证，请在内置页面重新登录。";
            return;
        }
        await ApplyCourseSessionAsync(sync);
        SaveSyncResult(sync);
    });

    private Task ForgetAccountAsync() => RunAsync("正在清除课程会话…", async () =>
    {
        await EnsureBrowserAsync();
        await ClearBrowserSessionAsync();
        store.EndSession(true);
        account = null;
        savedCookies = [];
        afterLogin = null;
        browseAfterLogin = null;
        queuedBrowse = null;
        noticeTarget = null;
        LoginBanner.Visibility = Visibility.Collapsed;
        LoadCatalog();
        ShowBoard();
        UpdateBoard();
        Status.Text = "已退出课程平台。";
    });

    private async Task ClearBrowserSessionAsync()
    {
        openRequest++;
        requestedCoursePage = null;
        queuedRecovery = null;
        activeNavigation = 0;
        SetBrowserFeedback("");
        web!.Stop();
        web.CookieManager.DeleteAllCookies();
        await web.Profile.ClearBrowsingDataAsync(Microsoft.Web.WebView2.Core.CoreWebView2BrowsingDataKinds.AllDomStorage);
    }

    private async Task ImportLegacyAsync()
    {
        var pick = new OpenFileDialog
        {
            Multiselect = true, Filter = "MongoDB JSON 导出文件|*.json;*.jsonl", Title = "选择 course_data、todos 和 assignment_status 的 JSON 导出"
        };
        if (pick.ShowDialog(this) != true)
            return;
        await RunAsync("正在导入旧版数据…", async () =>
        {
            try
            {
                var imported = await Task.Run(() => LegacyImport.Read(pick.FileNames), quit.Token);
                quit.Token.ThrowIfCancellationRequested();
                store.ImportTasks(imported);
                UpdateBoard();
                ShowBoard();
                Status.Text = $"导入完成，处理了 {imported.Count} 条任务，已有记录保留。";
            }
            catch (Exception ex) when (ex is FormatException or System.Text.Json.JsonException)
            {
                Status.Text = "导入失败。请选择同一用户的 course_data、todos 和 assignment_status 文件。";
            }
        });
    }
}
