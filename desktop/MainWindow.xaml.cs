using System.ComponentModel;
using System.Diagnostics;
using System.Security.Cryptography;
using System.Text.Json;
using System.Windows.Controls.Primitives;
using System.Windows.Threading;
using Microsoft.Web.WebView2.Core;
using Forms = System.Windows.Forms;

namespace BuctCourseLocal;

public partial class MainWindow : Window
{
    private readonly LocalStore store;
    private readonly string dataDir;
    private readonly CancellationTokenSource quit = new();
    private readonly DispatcherTimer timer = new() { Interval = TimeSpan.FromSeconds(30) };
    private readonly Forms.NotifyIcon tray;
    private readonly System.Drawing.Icon trayIcon;
    private List<CourseTask> tasks = [];
    private Credentials? account;
    private List<CourseCookie> savedCookies = [];
    private CoreWebView2? web;
    private Task? browserInitialization;
    private Uri? requestedCoursePage;
    private (Uri Target, long Request, int Session)? queuedRecovery;
    private ulong activeNavigation;
    private long openRequest;
    private int sessionGeneration;
    private bool retryExpiredSession;
    private Uri? afterLogin;
    private CourseTask? noticeTarget;
    private long nextSync;
    private long nextUrgencyChange = long.MaxValue;
    private TaskUrgency? urgencyFilter;
    private string? kindFilter;
    private bool syncAfterLogin;
    private bool browserLoginReady;
    private bool busy;
    private bool exiting;
    private int openDialogs;

    public MainWindow(string dataDir)
    {
        InitializeComponent();
        this.dataDir = dataDir;
        store = new LocalStore(dataDir);
        try
        {
            account = store.ReadSecret<Credentials>("account");
            savedCookies = store.ReadSecret<List<CourseCookie>>("cookies") ?? [];
        }
        catch (Exception ex) when (ex is CryptographicException or JsonException)
        {
            store.EndSession(false);
            account = null;
            Status.Text = "无法读取已保存的凭证。请重新登录课程平台。";
        }

        var menu = new Forms.ContextMenuStrip();
        menu.Items.Add("打开课程提醒", null, (_, _) => Dispatcher.Invoke(ShowFromTray));
        menu.Items.Add("立即同步", null, (_, _) => Dispatcher.Invoke(() => _ = RefreshAsync(true)));
        menu.Items.Add("退出", null, (_, _) => Dispatcher.Invoke(Exit));
        using (var iconStream = Application.GetResourceStream(new Uri("/BuctCourseLocal;component/Assets/CourseFlow.ico", UriKind.Relative)).Stream)
        using (var originalIcon = new System.Drawing.Icon(iconStream))
            trayIcon = (System.Drawing.Icon)originalIcon.Clone();
        tray = new Forms.NotifyIcon
        {
            Text = "北化课程提醒", Icon = trayIcon, ContextMenuStrip = menu, Visible = true
        };
        tray.MouseClick += (_, args) => { if (args.Button == Forms.MouseButtons.Left) Dispatcher.Invoke(ShowFromTray); };
        tray.BalloonTipClicked += (_, _) => Dispatcher.Invoke(() => _ = OpenNoticeAsync());
        Closing += HideToTray;
        Closed += DisposeWindow;
        UpdateBoard();
        ShowBoard();
        Loaded += (_, _) =>
        {
            if (timer.IsEnabled)
                return;
            timer.Tick += (_, _) => Tick();
            timer.Start();
            _ = Dispatcher.InvokeAsync(WarmBrowserAsync, DispatcherPriority.ApplicationIdle);
            _ = Dispatcher.InvokeAsync(Tick, DispatcherPriority.Background);
        };
    }

    private static long Now => DateTimeOffset.UtcNow.ToUnixTimeSeconds();
    private bool NoticesOn => store.Setting("notifications", 1) == 1;

    private void Tick()
    {
        try
        {
            if (Now >= nextUrgencyChange)
                FilterTasks();
            var taskNotified = NoticesOn && NotifyDue();
            TickCourseFlow(canShow: !taskNotified);
            if (!busy && openDialogs == 0 && afterLogin is null && (account is not null || savedCookies.Count > 0) && Now >= nextSync)
                _ = RefreshAsync(false);
        }
        catch (Exception ex)
        {
            Trace.TraceError("Reminder timer failed: {0}", ex.GetType().Name);
            Status.Text = "读取本地提醒失败。";
        }
    }

    private bool NotifyDue()
    {
        var due = store.DueNotices(Now);
        if (due.Count == 0)
            return false;
        noticeOpensSchedule = false;
        noticeTarget = due.Count == 1 ? due[0].Task : null;
        var first = due[0];
        var title = due.Count == 1 ? $"{first.Task.KindLabel}将在 {first.Hours} 小时内截止" : $"有 {due.Count} 项任务即将截止";
        var text = due.Count == 1 ? $"{first.Task.Title}\n{first.Task.DueLabel}" : $"{first.Task.Title} 等任务即将截止。点击查看任务列表。";
        tray.ShowBalloonTip(7000, title, text, Forms.ToolTipIcon.Info);
        store.MarkNotified(due);
        return true;
    }

    private async Task OpenNoticeAsync()
    {
        ShowFromTray();
        if (noticeOpensSchedule)
        {
            await ShowCourseFlowAsync();
            return;
        }
        ShowBoard();
        var task = noticeTarget is { } target ? tasks.FirstOrDefault(item => item.Id == target.Id && !item.Completed) : null;
        if (task?.IsTodo == true)
            EditTodo(task);
        else if (CourseService.TaskUri(task?.Url) is { } uri)
            await OpenCourseAsync(uri);
    }

    private void UpdateBoard()
    {
        tasks = store.Tasks();
        var lastSync = store.Setting("last_sync");
        LastSync.Text = lastSync > 0 ? $"上次同步 · {CourseTask.BeijingTime(lastSync):MM月dd日 HH:mm}" : "尚未同步课程";
        UpdateAccountStatus();
        NoticeStatus.Text = NoticesOn ? "截止通知已开启" : "截止通知已暂停";
        FilterTasks();
    }

    private void UpdateAccountStatus() => AccountStatus.Text = account is not null ? "已保存学校登录凭证" :
        savedCookies.Count > 0 ? "已保存课程会话" : "尚未登录课程平台";

    private void FilterTasks()
    {
        if (TaskList is null || EmptyHint is null || IncludeCompleted is null)
            return;
        var now = Now;
        var matchingKind = tasks.Where(t => string.IsNullOrEmpty(kindFilter) || t.Kind == kindFilter).ToList();
        var buckets = matchingKind.ToLookup(t => UrgencyBand.Of(t, now));
        UrgentCount.Text = buckets[TaskUrgency.Urgent].Count().ToString();
        UpcomingCount.Text = buckets[TaskUrgency.Upcoming].Count().ToString();
        NormalCount.Text = buckets[TaskUrgency.Normal].Count().ToString();
        UrgentFilter.IsChecked = urgencyFilter == TaskUrgency.Urgent;
        UpcomingFilter.IsChecked = urgencyFilter == TaskUrgency.Upcoming;
        NormalFilter.IsChecked = urgencyFilter == TaskUrgency.Normal;

        var groups = UrgencyBand.All
            .Where(band => (urgencyFilter is null || urgencyFilter == band.Urgency) &&
                (band.Urgency != TaskUrgency.Completed || IncludeCompleted.IsChecked == true))
            .Select(band => new TaskGroup(band, buckets[band.Urgency]
                .Select(task => new TaskCard(task, !task.Completed && task.Deadline > 0 && task.Deadline <= now)).ToList()))
            .Where(group => group.Count > 0).ToList();
        TaskList.ItemsSource = groups;
        Empty.Visibility = groups.Count == 0 ? Visibility.Visible : Visibility.Collapsed;
        EmptyHeadline.Text = tasks.Count > 0 ? "没有符合筛选的任务" : "暂无任务";
        EmptyHint.Text = tasks.Count > 0 ? "更改任务类型或时间筛选。" : "同步课程获取作业与测试，也可以新建个人待办。";
        nextUrgencyChange = matchingKind.Select(t => UrgencyBand.NextChange(t, now)).DefaultIfEmpty(long.MaxValue).Min();
    }

    private async Task RunAsync(string message, Func<Task> action, bool browserOperation = false)
    {
        if (busy || exiting)
        {
            if (!exiting)
                Status.Text = "正在处理课程请求，请稍候。";
            return;
        }
        busy = true;
        SyncButton.IsEnabled = false;
        if (message.Length > 0)
            Status.Text = message;
        try
        {
            await action();
        }
        catch (OperationCanceledException) when (exiting) { }
        catch (WebView2RuntimeNotFoundException)
        {
            ShowOperationError("找不到 Microsoft Edge WebView2 Runtime。安装后请重新打开应用。");
        }
        catch (Exception ex)
        {
            if (!exiting)
            {
                Trace.TraceError("Course request failed: {0}", ex.GetType().Name);
                ShowOperationError("操作未完成，请检查网络或本地数据后重试。");
            }
        }
        finally
        {
            busy = false;
            SyncButton.IsEnabled = !exiting;
            if (!exiting && browserLoginReady && afterLogin is not null)
            {
                browserLoginReady = false;
                _ = Dispatcher.BeginInvoke(() => _ = CompleteBrowserLoginAsync());
            }
            else if (!exiting && queuedRecovery is { } recovery)
            {
                queuedRecovery = null;
                _ = Dispatcher.BeginInvoke(() =>
                {
                    if (exiting || recovery.Request != openRequest || afterLogin is not null)
                        return;
                    if (recovery.Session != sessionGeneration)
                        NavigateCourse(recovery.Target, canRetry: false);
                    else
                        _ = RecoverCourseAsync(recovery.Target, recovery.Request);
                });
            }
            if (!exiting && queuedBrowse is { } browse)
            {
                queuedBrowse = null;
                _ = Dispatcher.BeginInvoke(() => _ = BrowseAsync(browse));
            }
        }

        void ShowOperationError(string error)
        {
            if (browserOperation)
                SetBrowserFeedback(error);
            else
                Status.Text = error;
        }
    }

    private Task RefreshAsync(bool interactive) => RunAsync("正在同步作业和测试…", async () =>
    {
        if (afterLogin is not null)
        {
            syncAfterLogin = true;
            ShowBrowser();
            Status.Text = "完成学校认证后将同步课程。";
            return;
        }
        nextSync = Now + 15 * 60;
        await EnsureBrowserAsync();
        var result = await RunCourseAsync("sync", account, await ReadCookiesAsync());
        if (result.NeedsLogin)
        {
            nextSync = Now + 3 * 3600;
            Status.Text = result.Error ?? "课程会话已失效，请在课程平台登录。";
            if (interactive)
                BeginBrowserLogin(new Uri(CourseService.HomeUrl), true);
            return;
        }
        await ApplyCourseSessionAsync(result);
        SaveSyncResult(result);
    });

    private void SaveSyncResult(CourseResult result)
    {
        if (result.Error is { } error)
        {
            Status.Text = error;
            return;
        }
        var incomplete = result.IncompleteKinds ?? [];
        store.SaveSnapshot(result.Tasks ?? [], incomplete, Now);
        nextSync = Now + (result.Warning is null ? 3 * 3600 : 15 * 60);
        UpdateBoard();
        Status.Text = result.Warning is { } warning
            ? $"已读取 {result.Tasks?.Count ?? 0} 项课程任务。{warning}，已保留相关缓存。"
            : $"同步完成，共 {result.Tasks?.Count ?? 0} 项课程任务。";
        if (NoticesOn)
            NotifyDue();
    }

    private async Task OpenCourseAsync(Uri target)
    {
        var request = ++openRequest;
        queuedRecovery = null;
        ShowBrowser();
        if (afterLogin is not null)
        {
            afterLogin = target;
            browseAfterLogin = null;
            SetBrowserFeedback("完成学校认证后将打开所选页面。");
            return;
        }

        requestedCoursePage = null;
        activeNavigation = 0;
        SetBrowserFeedback("正在打开课程页面…", loading: true);
        try
        {
            await EnsureBrowserAsync();
            var cookies = await ReadCookiesAsync(CourseService.ReminderUrl);
            if (request != openRequest || exiting)
                return;
            if (cookies.Any(c => c.Name.Equals("JSESSIONID", StringComparison.OrdinalIgnoreCase) &&
                                 !string.IsNullOrEmpty(c.Value) && (c.Expires == 0 || c.Expires > Now)))
                NavigateCourse(target, canRetry: true);
            else
                await RecoverCourseAsync(target, request);
        }
        catch (OperationCanceledException) when (exiting) { }
        catch (WebView2RuntimeNotFoundException) when (request == openRequest && !exiting)
        {
            SetBrowserFeedback("找不到 Microsoft Edge WebView2 Runtime。");
        }
        catch (Exception ex)
        {
            Trace.TraceError("Opening course page failed: {0}", ex.GetType().Name);
            if (request == openRequest && !exiting)
                SetBrowserFeedback("暂时无法打开课程页面，可以重试或返回任务列表。");
        }
    }

    private Task RecoverCourseAsync(Uri target, long request)
    {
        if (exiting || request != openRequest || afterLogin is not null)
            return Task.CompletedTask;
        if (account is null)
        {
            BeginBrowserLogin(target, false);
            return Task.CompletedTask;
        }
        SetBrowserFeedback("正在恢复课程会话…", loading: true);
        if (busy)
        {
            queuedRecovery = (target, request, sessionGeneration);
            return Task.CompletedTask;
        }

        return RunAsync("", async () =>
        {
            var result = await RunCourseAsync("login", account, []);
            if (!result.NeedsLogin && result.Error is null)
                await ApplyCourseSessionAsync(result);
            if (request != openRequest || exiting)
                return;
            if (result.NeedsLogin)
                BeginBrowserLogin(target, false);
            else if (result.Error is { } error)
                SetBrowserFeedback(error);
            else
                NavigateCourse(target, canRetry: false);
        }, browserOperation: true);
    }

    private void NavigateCourse(Uri target, bool canRetry)
    {
        requestedCoursePage = target;
        activeNavigation = 0;
        retryExpiredSession = canRetry;
        SetBrowserFeedback("正在打开课程页面…", loading: true);
        web!.Navigate(target.AbsoluteUri);
    }

    private void SetBrowserFeedback(string message, bool loading = false)
    {
        BrowserFeedback.Text = message;
        BrowserProgress.Visibility = loading ? Visibility.Visible : Visibility.Collapsed;
    }

    private async Task WarmBrowserAsync()
    {
        if (exiting)
            return;
        try { await EnsureBrowserAsync(); }
        catch (Exception ex) { Trace.TraceInformation("WebView2 warm-up deferred: {0}", ex.GetType().Name); }
    }

    private async Task EnsureBrowserAsync()
    {
        var initialization = browserInitialization ??= InitializeBrowserAsync();
        try { await initialization; }
        catch
        {
            if (browserInitialization == initialization)
            {
                browserInitialization = null;
                web = null;
            }
            throw;
        }
    }

    private async Task InitializeBrowserAsync()
    {
        if (CourseView.CoreWebView2 is null)
        {
            var environment = await CoreWebView2Environment.CreateAsync(userDataFolder: Path.Combine(dataDir, "WebView2"));
            await CourseView.EnsureCoreWebView2Async(environment);
        }
        quit.Token.ThrowIfCancellationRequested();
        web = CourseView.CoreWebView2 ?? throw new InvalidOperationException("WebView2 did not initialize.");
        web.Settings.IsWebMessageEnabled = false;
        web.Settings.AreHostObjectsAllowed = false;
        web.Settings.IsPasswordAutosaveEnabled = false;
        web.Settings.IsGeneralAutofillEnabled = false;
        var current = await ReadCookiesAsync();
        var missing = savedCookies.Where(saved => !current.Any(c => c.Name == saved.Name && c.Domain == saved.Domain && c.Path == saved.Path));
        await ApplyCookiesAsync(missing, persist: false);
        web.NavigationStarting += (_, args) =>
        {
            if (Uri.TryCreate(args.Uri, UriKind.Absolute, out var uri) && CourseService.IsSchoolUri(uri))
            {
                if (uri.Host == CourseService.Host && uri.AbsolutePath.Equals(CourseService.LegacyHomePath, StringComparison.OrdinalIgnoreCase))
                {
                    args.Cancel = true;
                    _ = Dispatcher.BeginInvoke(() => web.Navigate(CourseService.StudentWelcomeUrl));
                    return;
                }
                BrowserHost.Text = uri.Host;
                activeNavigation = args.NavigationId;
                if (requestedCoursePage is { } target && uri.Host is ("portal.buct.edu.cn" or "sidportal.buct.edu.cn"))
                {
                    afterLogin = target;
                    syncAfterLogin = false;
                    requestedCoursePage = null;
                    LoginBanner.Visibility = Visibility.Visible;
                    SetBrowserFeedback("请在内置页面完成学校认证。");
                }
                return;
            }
            args.Cancel = true;
            SetBrowserFeedback("此窗口仅打开学校的 HTTPS 页面。");
        };
        web.NavigationCompleted += async (_, args) =>
        {
            if (exiting || args.NavigationId != activeNavigation)
                return;
            if (afterLogin is not null && CourseService.TaskUri(web.Source) is { } uri &&
                (!uri.AbsolutePath.Equals(new Uri(CourseService.LoginUrl).AbsolutePath, StringComparison.OrdinalIgnoreCase) || uri.Query.Length > 0))
            {
                if (busy)
                    browserLoginReady = true;
                else
                    await CompleteBrowserLoginAsync();
                return;
            }
            if (requestedCoursePage is { } target)
            {
                var request = openRequest;
                var expired = args.HttpStatusCode == 401 || (args.IsSuccess && await IsSignedOutPageAsync());
                if (exiting || args.NavigationId != activeNavigation || request != openRequest)
                    return;
                requestedCoursePage = null;
                if (expired)
                {
                    if (retryExpiredSession)
                        await RecoverCourseAsync(target, request);
                    else
                        BeginBrowserLogin(target, false);
                    return;
                }
            }
            if (args.WebErrorStatus == CoreWebView2WebErrorStatus.OperationCanceled)
                return;
            SetBrowserFeedback(args.HttpStatusCode == 404 ? "该课程页面已失效（HTTP 404），可从课程平台重新进入。" :
                args.IsSuccess ? "" : "课程页面加载失败，可以重新加载。");
        };
        web.NewWindowRequested += (_, args) =>
        {
            args.Handled = true;
            if (Uri.TryCreate(args.Uri, UriKind.Absolute, out var uri) && CourseService.IsSchoolUri(uri))
                web.Navigate(uri.AbsoluteUri);
        };

    }

    private async Task<bool> IsSignedOutPageAsync()
    {
        if (CourseService.TaskUri(web!.Source) is null)
            return false;
        try
        {
            return await web.ExecuteScriptAsync("document.body?.innerText.slice(0, 3500).includes('用户身份错误，请重新登录') ?? false") == "true";
        }
        catch (InvalidOperationException) { return false; }
        catch (System.Runtime.InteropServices.COMException) { return false; }
    }

    private async Task<List<CourseCookie>> ReadCookiesAsync(string url = "")
    {
        var cookies = await web!.CookieManager.GetCookiesAsync(url);
        return cookies.Where(c => CourseService.IsCourseDomain(c.Domain)).Select(c =>
            new CourseCookie(c.Name, c.Value, c.Domain, c.Path,
                c.IsSession ? 0 : new DateTimeOffset(c.Expires.ToUniversalTime()).ToUnixTimeSeconds(),
                c.IsSecure, c.IsHttpOnly, c.SameSite.ToString())).ToList();
    }

    private Task<CourseResult> RunCourseAsync(string mode, Credentials? credentials, IReadOnlyList<CourseCookie> cookies,
        string courseId = "", string itemId = "") =>
        CourseService.RunAsync(mode, credentials, cookies, web!.Settings.UserAgent, quit.Token, courseId, itemId);

    private async Task ApplyCourseSessionAsync(CourseResult result)
    {
        if (result.NewSession)
        {
            var manager = web!.CookieManager;
            foreach (var cookie in await manager.GetCookiesAsync(""))
            {
                if (CourseService.IsCourseDomain(cookie.Domain))
                    manager.DeleteCookie(cookie);
            }
        }
        await ApplyCookiesAsync(result.Cookies);
        if (result.NewSession)
            sessionGeneration++;
    }

    private async Task ApplyCookiesAsync(IEnumerable<CourseCookie>? updates, bool persist = true)
    {
        var manager = web!.CookieManager;
        foreach (var item in updates ?? [])
        {
            if (!CourseService.IsCourseDomain(item.Domain))
                continue;
            if (item.Expires != 0 && item.Expires <= Now)
            {
                manager.DeleteCookiesWithDomainAndPath(item.Name, item.Domain, item.Path);
                continue;
            }
            var cookie = manager.CreateCookie(item.Name, item.Value, item.Domain, item.Path);
            cookie.IsHttpOnly = item.HttpOnly;
            cookie.IsSecure = item.Secure;
            if (item.Expires > 0)
                cookie.Expires = DateTimeOffset.FromUnixTimeSeconds(item.Expires).UtcDateTime;
            if (Enum.TryParse<CoreWebView2CookieSameSiteKind>(item.SameSite, out var sameSite))
                cookie.SameSite = sameSite;
            manager.AddOrUpdateCookie(cookie);
        }
        if (persist)
        {
            savedCookies = await ReadCookiesAsync();
            store.SaveSecret("cookies", savedCookies);
            UpdateAccountStatus();
        }
    }

    private void BeginBrowserLogin(Uri target, bool sync)
    {
        afterLogin = target;
        requestedCoursePage = null;
        queuedRecovery = null;
        activeNavigation = 0;
        syncAfterLogin = sync;
        browserLoginReady = false;
        LoginBanner.Visibility = Visibility.Visible;
        ShowBrowser();
        SetBrowserFeedback("正在打开学校认证…", loading: true);
        web!.Navigate(CourseService.LoginUrl);
    }

    private Task CompleteBrowserLoginAsync() => RunAsync("", async () =>
    {
        if (afterLogin is null)
            return;
        SetBrowserFeedback("正在确认课程登录…", loading: true);
        var cookies = await ReadCookiesAsync();
        var result = await RunCourseAsync("session", null, cookies);
        if (result.NeedsLogin || result.Error is not null)
        {
            SetBrowserFeedback(result.Error ?? "尚未完成课程平台登录，请继续认证。");
            return;
        }
        await ApplyCourseSessionAsync(result);
        if (exiting || afterLogin is not { } target)
            return;
        afterLogin = null;
        browserLoginReady = false;
        LoginBanner.Visibility = Visibility.Collapsed;
        if (browseAfterLogin is { } resume)
        {
            browseAfterLogin = null;
            if (resume.FromCatalog)
                ShowCatalog();
            else
                ShowBoard();
            queuedBrowse = resume;
            nextSync = Now;
            return;
        }
        var landing = CourseService.TaskUri(web!.Source);
        if (target.AbsoluteUri != CourseService.HomeUrl || landing is null ||
            landing.AbsolutePath.StartsWith("/meol/homepage/common/sso_login.jsp", StringComparison.OrdinalIgnoreCase))
            NavigateCourse(target, canRetry: false);
        else
            SetBrowserFeedback("");
        nextSync = Now;
        if (syncAfterLogin)
        {
            var syncResult = await RunCourseAsync("sync", null, await ReadCookiesAsync());
            if (syncResult.NeedsLogin)
                Status.Text = "会话已失效，请重新登录。";
            else
            {
                await ApplyCourseSessionAsync(syncResult);
                SaveSyncResult(syncResult);
            }
        }
    }, browserOperation: true);

    private void ShowBrowser()
    {
        Board.Visibility = Visibility.Collapsed;
        CatalogPanel.Visibility = Visibility.Collapsed;
        SchedulePanel.Visibility = Visibility.Collapsed;
        BrowserPanel.Visibility = Visibility.Visible;
        HighlightNav(PortalButton);
    }

    private void ShowBoard()
    {
        BrowserPanel.Visibility = Visibility.Collapsed;
        CatalogPanel.Visibility = Visibility.Collapsed;
        SchedulePanel.Visibility = Visibility.Collapsed;
        Board.Visibility = Visibility.Visible;
        HighlightNav(BoardButton);
    }

    private void HighlightNav(Button active)
    {
        foreach (var button in new[] { BoardButton, CatalogButton, ScheduleButton, PortalButton })
            button.Tag = button == active ? "Selected" : null;
    }

    private void ShowFromTray()
    {
        Show();
        if (WindowState == WindowState.Minimized)
            WindowState = WindowState.Normal;
        Activate();
    }

    private void HideToTray(object? sender, CancelEventArgs e)
    {
        if (exiting)
            return;
        e.Cancel = true;
        Hide();
    }

    private void Exit()
    {
        exiting = true;
        Application.Current.Shutdown();
    }

    private void DisposeWindow(object? sender, EventArgs e)
    {
        exiting = true;
        quit.Cancel();
        timer.Stop();
        tray.Visible = false;
        tray.ContextMenuStrip?.Dispose();
        tray.Dispose();
        trayIcon.Dispose();
        CourseView.Dispose();
        store.Dispose();
    }

    private void Board_Click(object sender, RoutedEventArgs e) => ShowBoard();
    private async void Portal_Click(object sender, RoutedEventArgs e)
    {
        if (afterLogin is not null)
            ShowBrowser();
        else
            await OpenCourseAsync(new Uri(CourseService.HomeUrl));
    }
    private async void Sync_Click(object sender, RoutedEventArgs e) => await RefreshAsync(true);
    private void Filter_Changed(object sender, RoutedEventArgs e) => FilterTasks();
    private void Kind_Checked(object sender, RoutedEventArgs e)
    {
        kindFilter = (sender as RadioButton)?.Tag as string;
        FilterTasks();
    }
    private void Urgency_Click(object sender, RoutedEventArgs e)
    {
        if (sender is ToggleButton { Tag: TaskUrgency selected })
        {
            urgencyFilter = urgencyFilter == selected ? null : selected;
            FilterTasks();
        }
    }
    private void Browser_Back(object sender, RoutedEventArgs e) { if (web?.CanGoBack == true) web.GoBack(); }
    private void Browser_Reload(object sender, RoutedEventArgs e) => web?.Reload();
    private async void ConfirmBrowserLogin_Click(object sender, RoutedEventArgs e) => await CompleteBrowserLoginAsync();
    private void AddTodo_Click(object sender, RoutedEventArgs e) => EditTodo(null);

    private void ShowDialog(EditDialog dialog)
    {
        openDialogs++;
        try { dialog.ShowDialog(); }
        finally { openDialogs--; }
    }

    private async void Task_Open(object sender, RoutedEventArgs e)
    {
        if (sender is not Button { DataContext: CourseTask task })
            return;
        if (task.IsTodo)
            EditTodo(task);
        else if (CourseService.TaskUri(task.Url) is { } uri)
            await OpenCourseAsync(uri);
    }

    private void Task_Toggle(object sender, RoutedEventArgs e)
    {
        if (sender is Button { DataContext: CourseTask task })
        {
            store.SetCompleted(task.Id, !task.Completed);
            UpdateBoard();
        }
    }

    private void EditTodo(CourseTask? task)
    {
        var dialog = new EditDialog(this, task is null ? "新建待办" : "编辑待办");
        var title = new TextBox { Text = task?.Title ?? "", MaxLength = 180 };
        var due = new TextBox { Text = task is { Deadline: > 0 } ? CourseTask.BeijingTime(task.Deadline).ToString("yyyy-MM-dd HH:mm") : "", MaxLength = 16 };
        var details = new TextBox { Text = task?.Details ?? "", AcceptsReturn = true, TextWrapping = TextWrapping.Wrap, Height = 80, VerticalScrollBarVisibility = ScrollBarVisibility.Auto };
        dialog.AddField("标题", title);
        dialog.AddField("截止时间（北京时间，可留空。格式 yyyy-MM-dd HH:mm）", due);
        dialog.AddField("备注", details);
        dialog.AddAction("取消", dialog.Close, cancel: true);
        if (task is not null)
            dialog.AddAction("删除", () =>
            {
                HideTask(task);
                dialog.Close();
            });
        dialog.AddAction("保存", () =>
        {
            if (string.IsNullOrWhiteSpace(title.Text) || !CourseTask.TryParseDue(due.Text, out var deadline))
            {
                dialog.ShowError("请填写标题，并检查截止时间格式。");
                return;
            }
            store.SaveTodo(new CourseTask(task?.Id ?? $"todo:{Guid.NewGuid():N}", "todo", "个人", title.Text.Trim(), details.Text.Trim(), "", deadline));
            UpdateBoard();
            dialog.Close();
        }, primary: true);
        dialog.Loaded += (_, _) => title.Focus();
        ShowDialog(dialog);
    }
}
