namespace BuctCourseLocal;

public partial class MainWindow
{
    private sealed record BrowseRequest(string Mode, string Key, string CourseId = "", string ItemId = "", string Title = "",
        string FallbackText = "", string Page = "", bool FromCatalog = true);

    private List<CatalogCourse> catalog = [];
    private bool bindingCatalog;
    private string catalogMode = "course-homework";
    private BrowseRequest? queuedBrowse;
    private BrowseRequest? browseAfterLogin;

    private void ShowCatalog() => ShowPage(CatalogPanel, CatalogButton);

    private async void Catalog_Click(object sender, RoutedEventArgs e)
    {
        ShowCatalog();
        LoadCatalog();
        if (store.CourseCache<List<CatalogCourse>>("catalog") is null && (account is not null || HasSavedCourseSession) && afterLogin is null)
            await BrowseAsync(new BrowseRequest("catalog", "catalog"));
    }

    private async void Catalog_Refresh_Click(object sender, RoutedEventArgs e) => await BrowseAsync(new BrowseRequest("catalog", "catalog"));

    private void LoadCatalog(string? warning = null)
    {
        var saved = store.CourseCache<List<CatalogCourse>>("catalog");
        var fromTasks = store.Tasks(includeHidden: true)
            .Where(t => !t.IsTodo && t.CourseId.Length > 0)
            .Select(t => new CatalogCourse(t.CourseId, t.Subject,
                $"https://{CourseService.Host}/meol/jpk/course/layout/newpage/index.jsp?courseId={t.CourseId}", "", ""));
        catalog = (saved?.AsEnumerable() ?? fromTasks).DistinctBy(c => c.Id).OrderBy(c => c.Name).ToList();
        CatalogSummary.Text = warning ?? (saved is not null ? $"已缓存 {catalog.Count} 门课程" : catalog.Count > 0 ?
            "当前显示待办中已知的课程，刷新可查询课程目录。" : "点击刷新课程，从北化在线获取目录。");
        BindCatalog();
    }

    private void BindCatalog()
    {
        if (CatalogList is null || CatalogSearch is null)
            return;
        var selected = (CatalogList.SelectedItem as CatalogCourse)?.Id;
        var query = CatalogSearch.Text.Trim();
        var visible = catalog.Where(c => c.Name.Contains(query, StringComparison.CurrentCultureIgnoreCase) ||
            c.Metadata.Contains(query, StringComparison.CurrentCultureIgnoreCase)).ToList();
        bindingCatalog = true;
        try
        {
            CatalogList.ItemsSource = visible;
            CatalogList.SelectedItem = visible.FirstOrDefault(c => c.Id == selected);
        }
        finally { bindingCatalog = false; }
        RenderCourseItems();
    }

    private void CatalogSearch_Changed(object sender, TextChangedEventArgs e) => BindCatalog();

    private async void Catalog_SelectionChanged(object sender, SelectionChangedEventArgs e)
    {
        if (!bindingCatalog)
            await SelectCourseItemsAsync();
    }

    private async void CatalogTab_Changed(object sender, RoutedEventArgs e)
    {
        if (sender is RadioButton { Tag: string mode })
            catalogMode = mode;
        if (CatalogEntries is not null)
            await SelectCourseItemsAsync();
    }

    private void CatalogAvailable_Changed(object sender, RoutedEventArgs e) => RenderCourseItems();
    private async void Catalog_ReloadItems(object sender, RoutedEventArgs e) => await SelectCourseItemsAsync(force: true);

    private async Task SelectCourseItemsAsync(bool force = false)
    {
        RenderCourseItems();
        if (CatalogList.SelectedItem is not CatalogCourse course)
            return;
        var key = $"{catalogMode}:{course.Id}";
        if (force || store.CourseCache<List<CatalogEntry>>(key) is null)
            await BrowseAsync(new BrowseRequest(catalogMode, key, course.Id));
    }

    private void RenderCourseItems()
    {
        if (CatalogEntries is null)
            return;
        if (CatalogList.SelectedItem is not CatalogCourse course)
        {
            CatalogCourseTitle.Text = "选择一门课程";
            CatalogEntryNote.Text = "";
            CatalogEntries.ItemsSource = null;
            CatalogOpenButton.IsEnabled = false;
            return;
        }
        CatalogCourseTitle.Text = course.Name;
        CatalogOpenButton.IsEnabled = CourseService.TaskUri(course.Url) is not null;
        var stored = store.CourseCache<List<CatalogEntry>>($"{catalogMode}:{course.Id}");
        var hidden = store.Filters().ToLookup(f => f.Kind, f => f.Key);
        var blocked = hidden["course"].Contains($"id:{course.Id}") || hidden["course"].Contains($"name:{course.Name}");
        var visible = (stored ?? []).Where(x => !blocked && !hidden["task"].Contains(x.Id) &&
            (CatalogAvailableOnly.IsChecked != true || x.Available)).ToList();
        CatalogEntries.ItemsSource = visible;
        CatalogEntryNote.Text = blocked ? "此课程已屏蔽，可从隐藏与回收站恢复。" : stored is null ? "尚未读取这个列表。" :
            visible.Count > 0 ? $"{visible.Count} 项" : "当前筛选条件下没有项目。";
    }

    private Task BrowseAsync(BrowseRequest request)
    {
        if (exiting)
            return Task.CompletedTask;
        if (busy)
        {
            queuedBrowse = request;
            SetStatus("当前同步结束后将读取所选内容。");
            return Task.CompletedTask;
        }

        return RunAsync("正在读取课程平台…", async () =>
        {
            var navigation = openRequest;
            await EnsureBrowserAsync();
            var result = await RunCourseAsync(request.Mode, account, await ReadCookiesAsync(), request.CourseId, request.ItemId);
            if (result.NeedsLogin)
            {
                if (navigation != openRequest)
                    return;
                browseAfterLogin = request;
                if (afterLogin is null)
                    BeginBrowserLogin(new Uri(CourseService.HomeUrl), false);
                else
                    ShowBrowser();
                return;
            }
            await ApplyCourseSessionAsync(result);
            if (result.Error is { } error)
            {
                SetStatus(error, () => BrowseAsync(request));
                if (request.FallbackText.Length > 0 && IsBrowseVisible(request))
                    ShowItemDetail(request, EmptyDetail(request.FallbackText));
                return;
            }

            if (request.Mode == "catalog")
            {
                var courses = result.Courses ?? [];
                if (result.Warning is not null)
                    courses = (store.CourseCache<List<CatalogCourse>>(request.Key) ?? []).Concat(courses).DistinctBy(c => c.Id).ToList();
                if (result.Warning is null || courses.Count > 0)
                    store.SaveCourseCache(request.Key, courses);
                LoadCatalog(result.Warning);
            }
            else if (request.Mode is "course-homework" or "course-tests")
            {
                store.SaveCourseCache(request.Key, result.Entries ?? []);
                if (IsBrowseVisible(request))
                    RenderCourseItems();
            }
            else if (result.Detail is { } detail)
            {
                store.SaveCourseCache(request.Key, detail);
                if (IsBrowseVisible(request) && queuedBrowse is null)
                    ShowItemDetail(request, detail);
            }
            SetStatus(result.Warning ?? "读取完成。");
        }, retry: () => BrowseAsync(request));
    }

    private bool IsBrowseVisible(BrowseRequest request) => request.FromCatalog ?
        CatalogPanel.Visibility == Visibility.Visible && (CatalogList.SelectedItem as CatalogCourse)?.Id == request.CourseId :
        Board.Visibility == Visibility.Visible;

    private Task OpenDetailAsync(BrowseRequest request)
    {
        if (store.CourseCache<ItemDetail>(request.Key) is { } cached)
        {
            ShowItemDetail(request, cached);
            return Task.CompletedTask;
        }
        return BrowseAsync(request);
    }

    private Task ViewDetailAsync(string kind, string courseId, string itemId, string title, string page,
        string fallbackText = "", bool fromCatalog = true) => OpenDetailAsync(new BrowseRequest($"{kind}-detail",
            $"{kind}-detail:{courseId}:{itemId}", courseId, itemId, title, fallbackText, page, fromCatalog));

    private async void Catalog_ItemDetail(object sender, RoutedEventArgs e)
    {
        if (sender is Button { DataContext: CatalogEntry item } && CatalogList.SelectedItem is CatalogCourse course && item.HasDetail)
            await ViewDetailAsync(item.Kind, course.Id, item.ItemId, item.Title, item.Url);
    }

    private async void Catalog_OpenCourse(object sender, RoutedEventArgs e)
    {
        if (CatalogList.SelectedItem is CatalogCourse selected && CourseService.TaskUri(selected.Url) is { } uri)
            await OpenCourseAsync(uri);
    }

    private async void Catalog_ItemOpen(object sender, RoutedEventArgs e)
    {
        if (sender is Button { DataContext: CatalogEntry item } && CourseService.TaskUri(item.Url) is { } uri)
            await OpenCourseAsync(uri);
    }

    private async void Catalog_ItemResult(object sender, RoutedEventArgs e)
    {
        if (sender is Button { DataContext: CatalogEntry item } && CourseService.TaskUri(item.ResultUrl) is { } uri)
            await OpenCourseAsync(uri);
    }

    private void Catalog_ItemHide(object sender, RoutedEventArgs e)
    {
        if (sender is Button { DataContext: CatalogEntry item })
            ApplyTaskFilter(new TaskFilter(item.Id, item.Title, "task"), true);
    }

    private static ItemDetail EmptyDetail(string text) => new("", text, "", "", "", "", "", "", null);

    private void ShowItemDetail(BrowseRequest request, ItemDetail detail)
    {
        var dialog = new EditDialog(this, request.Mode == "homework-detail" ? "作业详情" : "测试详情");
        dialog.AddDescription(string.IsNullOrWhiteSpace(detail.Title) ? request.Title : detail.Title);
        var metadata = new (string Label, string Value)[]
        {
            ("开始", detail.Start), ("截止", detail.Deadline), ("时长", detail.Duration), ("题目数", detail.Questions),
            (request.Mode == "test-detail" ? "总分" : "成绩", detail.Score), ("发布人", detail.Publisher)
        };
        foreach (var (label, value) in metadata.Where(row => !string.IsNullOrWhiteSpace(row.Value)))
            dialog.AddDescription($"{label} · {value}");
        if (!string.IsNullOrWhiteSpace(detail.Text))
            dialog.AddField("正文", new TextBox { Text = detail.Text, TextWrapping = TextWrapping.Wrap, IsReadOnly = true, Height = 145,
                VerticalScrollBarVisibility = ScrollBarVisibility.Auto });
        Uri? target = null;
        if (detail.Attachments is { Count: > 0 } attachments)
        {
            var files = new ListBox { ItemsSource = attachments, DisplayMemberPath = "Name", MaxHeight = 110 };
            dialog.AddField("附件", files);
            var open = new Button { Content = "打开选中的附件", IsEnabled = false };
            dialog.AddOption(open);
            files.SelectionChanged += (_, _) => open.IsEnabled = files.SelectedItem is CourseAttachment selected && CourseService.TaskUri(selected.Url) is not null;
            open.Click += (_, _) =>
            {
                target = CourseService.TaskUri((files.SelectedItem as CourseAttachment)?.Url);
                dialog.Close();
            };
        }
        var refresh = false;
        dialog.AddAction("关闭", dialog.Close, primary: true);
        dialog.AddAction("刷新详情", () => { refresh = true; dialog.Close(); });
        if (CourseService.TaskUri(request.Page) is { } page)
            dialog.AddAction("打开学校页面", () => { target = page; dialog.Close(); });
        ShowDialog(dialog);
        if (target is not null)
            _ = OpenCourseAsync(target);
        else if (refresh)
            _ = BrowseAsync(request);
    }
}
