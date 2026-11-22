using System.Diagnostics;
using System.Globalization;
using System.Text.Json;

namespace BuctCourseLocal;

public partial class MainWindow
{
    private DashboardSchedule? dashboardSchedule;
    private bool dashboardLoading;
    private long dashboardRetryAt;
    private long dashboardMinute = -1;

    private sealed record DashboardRow(string Title, string Detail, string Date = "");
    private sealed record DashboardTask(CourseTask Task)
    {
        public string Detail => string.Join(" · ", new[] { Task.KindLabel, Task.Subject, Task.DueLabel }
            .Where(value => !string.IsNullOrWhiteSpace(value)));
        public bool CanOpen => Task.IsTodo || Task.HasLink;
    }

    private void Dashboard_Click(object sender, RoutedEventArgs e)
    {
        ShowPage(DashboardPanel, DashboardButton);
        dashboardMinute = -1;
        dashboardRetryAt = 0;
        RefreshDashboard();
    }

    private void RefreshDashboard()
    {
        var now = Now;
        if (dashboardMinute != now / 60)
        {
            dashboardMinute = now / 60;
            RenderDashboardTasks();
            RenderDashboardSchedule(CourseTask.BeijingTime(now));
        }

        var local = CourseTask.BeijingTime(now);
        var today = ScheduleDates.Iso(DateOnly.FromDateTime(local.DateTime));
        var first = dashboardSchedule?.Exams.FirstOrDefault();
        var examStarted = first is not null && string.CompareOrdinal($"{first.Date} {first.Start}",
            local.ToString("yyyy-MM-dd HH:mm", CultureInfo.InvariantCulture)) <= 0;
        if (!dashboardLoading && now >= dashboardRetryAt && (dashboardSchedule?.Today.Date != today || examStarted))
            _ = LoadDashboardScheduleAsync();
    }

    private async Task LoadDashboardScheduleAsync()
    {
        dashboardLoading = true;
        try
        {
            var snapshot = await GetCourseFlowAsync();
            using var json = JsonDocument.Parse(snapshot.Json);
            var result = await ReadCourseFlowWorkerAsync(new CourseFlowRequest(json.RootElement, Dashboard: true));
            if (exiting || store.ReadCourseFlow()?.Revision != snapshot.Revision)
                return;
            dashboardSchedule = result.Dashboard ?? throw new InvalidOperationException("工作台缺少课表数据。");
            dashboardRetryAt = 0;
            RenderDashboardSchedule(CourseTask.BeijingTime(Now));
        }
        catch (OperationCanceledException) when (exiting) { }
        catch (Exception ex)
        {
            if (exiting)
                return;
            Trace.TraceError("Dashboard schedule failed: {0}", ex.GetType().Name);
            dashboardRetryAt = Now + 60;
            DashboardTodayHint.Text = "无法读取本地课表。";
        }
        finally { dashboardLoading = false; }
    }

    private void RenderDashboardTasks()
    {
        var now = Now;
        var pending = tasks.Where(task => !task.Completed).ToList();
        var overdue = pending.Count(task => task.Deadline > 0 && task.Deadline <= now);
        var dueSoon = pending.Where(task => task.Deadline > now && task.Deadline - now <= 3 * UrgencyBand.Day)
            .OrderBy(task => task.Deadline).ToList();
        DashboardTaskSummary.Text = $"待办 {pending.Count} 项 · 逾期 {overdue} 项 · 未来 3 天到期 {dueSoon.Count} 项";
        DashboardTasks.ItemsSource = dueSoon.Take(5).Select(task => new DashboardTask(task)).ToList();
    }

    private void RenderDashboardSchedule(DateTimeOffset now)
    {
        var today = DateOnly.FromDateTime(now.DateTime);
        var dayLabel = $"{today:yyyy年M月d日} · {ScheduleDates.DayName(ScheduleDates.DayNumber(today))}";
        if (dashboardSchedule is not { } schedule || schedule.Today.Date != ScheduleDates.Iso(today))
        {
            DashboardDate.Text = dayLabel;
            DashboardTodayHint.Text = dashboardRetryAt > Now ? "无法读取本地课表。" : "正在读取课表…";
            DashboardToday.ItemsSource = null;
            DashboardExams.ItemsSource = null;
            DashboardExamHint.Text = "";
            return;
        }

        DashboardDate.Text = schedule.TermWeek > 0
            ? $"{dayLabel} · {schedule.TermName} · 第 {schedule.TermWeek} 周"
            : $"{dayLabel} · {schedule.TermName}";
        var time = now.ToString("HH:mm", CultureInfo.InvariantCulture);
        var next = schedule.Today.Entries.FirstOrDefault(entry => !entry.Exam && string.CompareOrdinal(entry.Start, time) > 0);
        DashboardTodayHint.Text = schedule.Today.Entries.Count == 0 ? "今天没有安排。" : "";
        DashboardToday.ItemsSource = schedule.Today.Entries.Select(entry => new DashboardRow(
            entry == next ? $"下一节 · {entry.Title}" : entry.Title,
            JoinDashboardDetail(entry.TimeLabel, entry.Metadata))).ToList();

        var stamp = $"{schedule.Today.Date} {time}";
        var exams = schedule.Exams.Where(entry => string.CompareOrdinal($"{entry.Date} {entry.Start}", stamp) > 0)
            .Select(entry =>
            {
                var date = ScheduleDates.Parse(entry.Date);
                var days = date.DayNumber - today.DayNumber;
                var label = days == 0 ? "今天" : $"还有 {days} 天";
                return new DashboardRow(entry.Name, JoinDashboardDetail($"{label} · {date:MM/dd} {entry.TimeLabel}", entry.Metadata), entry.Date);
            }).ToList();
        DashboardExamHint.Text = exams.Count == 0 ? "本地课表暂无待考安排。" : "";
        DashboardExams.ItemsSource = exams;
    }

    private static string JoinDashboardDetail(string time, string metadata) =>
        metadata.Length == 0 ? time : $"{time} · {metadata}";

    private async void Dashboard_Today(object sender, RoutedEventArgs e)
    {
        scheduleDayView = true;
        scheduleDate = null;
        await ShowCourseFlowAsync();
    }

    private async void Dashboard_Exam(object sender, RoutedEventArgs e)
    {
        if (sender is not Button { DataContext: DashboardRow exam })
            return;
        scheduleDayView = true;
        scheduleDate = ScheduleDates.Parse(exam.Date);
        await ShowCourseFlowAsync();
    }
}
