using System.Diagnostics;
using System.Text.Json;
using Forms = System.Windows.Forms;

namespace BuctCourseLocal;

internal record CourseFlowSnapshot(long Revision, string Json);
internal record CourseFlowNotice(string Key, string Title, string Body, long At);
internal record LegacyCourseFlowEntry(string Id, string Title, long Start, long End, string Location, string Description);
internal record CourseFlowRequest(JsonElement? State = null, List<LegacyCourseFlowEntry>? Legacy = null, bool Advance = false, string Week = "", bool Export = false);
internal record CourseFlowResult(JsonElement? State, List<CourseFlowNotice>? Reminders, string? Error,
    ScheduleWeekView? Week = null, List<ScheduleEventView>? Events = null);

public partial class MainWindow
{
    private const int CourseFlowLimit = 2 << 20;
    private Task<CourseFlowSnapshot>? courseFlowImport;
    private List<CourseFlowNotice> courseFlowNotices = [];
    private long courseFlowRefreshAt;
    private bool courseFlowRefreshInProgress;
    private bool noticeOpensSchedule;

    private static string CourseFlowOriginalPath => Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData), "CourseFlow", "schedule.json");

    private async Task<CourseFlowSnapshot> GetCourseFlowAsync()
    {
        if (store.ReadCourseFlow() is { } stored)
            return stored;
        var loading = courseFlowImport ??= ImportCourseFlowAsync();
        try { return await loading; }
        finally { if (courseFlowImport == loading) courseFlowImport = null; }
    }

    private async Task<CourseFlowSnapshot> ImportCourseFlowAsync()
    {
        CourseFlowRequest request;
        var source = CourseFlowOriginalPath;
        if (File.Exists(source))
        {
            if (new FileInfo(source).Length > CourseFlowLimit)
                throw new FormatException("原有课序数据超过 2 MB，请检查课表文件。");
            using var data = JsonDocument.Parse(await File.ReadAllTextAsync(source, quit.Token));
            request = new CourseFlowRequest(data.RootElement.Clone());
        }
        else
            request = new CourseFlowRequest(Legacy: store.CourseCache<List<LegacyCourseFlowEntry>>("schedule"));
        var (snapshot, notices) = await CheckCourseFlowAsync(request);
        quit.Token.ThrowIfCancellationRequested();
        if (!store.SaveCourseFlow(snapshot, null))
            return store.ReadCourseFlow() ?? throw new IOException("课表初始化冲突。");
        UpdateCourseFlowNotices(notices);
        return snapshot;
    }

    private async Task<CourseFlowSnapshot?> WriteCourseFlowAsync(JsonElement state)
    {
        if (state.ValueKind != JsonValueKind.Object || !state.TryGetProperty("revision", out var version) ||
            version.ValueKind != JsonValueKind.Number || !version.TryGetInt64(out var expected))
            throw new FormatException("课表版本无效。");
        if ((await GetCourseFlowAsync()).Revision != expected)
            return null;
        var (saved, notices) = await CheckCourseFlowAsync(new CourseFlowRequest(state, Advance: true));
        quit.Token.ThrowIfCancellationRequested();
        if (!store.SaveCourseFlow(saved, expected))
            return null;
        UpdateCourseFlowNotices(notices);
        return saved;
    }

    private async Task<(CourseFlowSnapshot Snapshot, List<CourseFlowNotice> Notices)> CheckCourseFlowAsync(CourseFlowRequest request)
    {
        var result = await ReadCourseFlowWorkerAsync(request);
        var data = result.State ?? throw new IOException("课表结果为空。");
        return (new CourseFlowSnapshot(data.GetProperty("revision").GetInt64(), data.GetRawText()), result.Reminders ?? []);
    }

    private async Task<CourseFlowResult> ReadCourseFlowWorkerAsync(CourseFlowRequest request)
    {
        var result = await CourseService.RunWorkerAsync<CourseFlowRequest, CourseFlowResult>(request, quit.Token, TimeSpan.FromSeconds(8), "schedule");
        if (result.Error is { } error)
            throw new FormatException(error);
        return result;
    }

    private void UpdateCourseFlowNotices(List<CourseFlowNotice> reminders)
    {
        courseFlowNotices = reminders;
        courseFlowRefreshAt = Now + 12 * 3600;
    }

    private void TickCourseFlow(bool canShow)
    {
        if (Now >= courseFlowRefreshAt && !courseFlowRefreshInProgress)
            _ = RefreshCourseFlowNoticesAsync();
        if (!canShow)
            return;
        var now = Now;
        courseFlowNotices.RemoveAll(n => n.At <= now - 300);
        var notice = courseFlowNotices.FirstOrDefault(n => n.At <= now && !store.HasCourseFlowNotice(n.Key));
        if (notice is null)
            return;
        ShowCourseFlowNotice(notice.Title, notice.Body);
        store.MarkCourseFlowNotice(notice.Key, now);
        courseFlowNotices.Remove(notice);
    }

    private async Task RefreshCourseFlowNoticesAsync()
    {
        courseFlowRefreshInProgress = true;
        courseFlowRefreshAt = Now + 15 * 60;
        try
        {
            if (store.ReadCourseFlow() is not { } snapshot)
            {
                if (File.Exists(CourseFlowOriginalPath) || store.CourseCache<List<LegacyCourseFlowEntry>>("schedule") is { Count: > 0 })
                    await GetCourseFlowAsync();
                return;
            }
            using var content = JsonDocument.Parse(snapshot.Json);
            var (_, notices) = await CheckCourseFlowAsync(new CourseFlowRequest(content.RootElement));
            if (!exiting && store.ReadCourseFlow()?.Revision == snapshot.Revision)
                UpdateCourseFlowNotices(notices);
        }
        catch (Exception ex)
        {
            if (!exiting)
                Trace.TraceError("CourseFlow reminders failed: {0}", ex.GetType().Name);
        }
        finally { courseFlowRefreshInProgress = false; }
    }

    private void ShowCourseFlowNotice(string title, string body)
    {
        noticeOpensSchedule = true;
        noticeTarget = null;
        tray.ShowBalloonTip(7000, title, body, Forms.ToolTipIcon.Info);
    }
}
