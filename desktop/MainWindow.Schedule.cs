using System.Diagnostics;
using System.Text.Json;

namespace BuctCourseLocal;

public partial class MainWindow
{
    private DateOnly? scheduleDate;
    private bool scheduleDayView;
    private ScheduleState? scheduleState;
    private int scheduleRenderVersion;

    private async void Schedule_Click(object sender, RoutedEventArgs e) => await ShowCourseFlowAsync();

    private async Task ShowCourseFlowAsync()
    {
        ShowPage(SchedulePanel, ScheduleButton);
        await RenderScheduleAsync();
    }

    private async void Schedule_Today(object sender, RoutedEventArgs e)
    {
        scheduleDate = null;
        await RenderScheduleAsync();
    }

    private async void Schedule_Move(object sender, RoutedEventArgs e)
    {
        if (sender is not Button { Tag: string direction } || !int.TryParse(direction, out var step))
            return;
        scheduleDate = (scheduleDate ?? ScheduleDates.Today).AddDays(step * (scheduleDayView ? 1 : 7));
        await RenderScheduleAsync();
    }

    private async void Schedule_SwitchView(object sender, RoutedEventArgs e)
    {
        scheduleDayView = !scheduleDayView;
        await RenderScheduleAsync();
    }

    private async Task RenderScheduleAsync()
    {
        var version = ++scheduleRenderVersion;
        var date = scheduleDate ?? ScheduleDates.Today;
        var monday = ScheduleDates.Monday(date);
        var dayView = scheduleDayView;
        ScheduleDays.Tag = dayView ? 1 : 7;
        ScheduleDays.MinWidth = dayView ? 0 : 600;
        ScheduleViewButton.Content = dayView ? "周视图" : "日视图";
        ScheduleTodayButton.Content = dayView ? "今天" : "本周";
        System.Windows.Automation.AutomationProperties.SetName(SchedulePrevious, dayView ? "前一天" : "上一周");
        System.Windows.Automation.AutomationProperties.SetName(ScheduleNextButton, dayView ? "后一天" : "下一周");
        ScheduleWeekLabel.Text = dayView ? $"{date:yyyy/MM/dd} · {ScheduleDates.DayName(ScheduleDates.DayNumber(date))}"
            : $"{monday:yyyy/MM/dd} – {monday.AddDays(6):MM/dd}";
        ScheduleDays.ItemsSource = null;
        ScheduleNext.Text = "";
        CourseFlowStatus.Text = "正在读取课表…";
        try
        {
            var snapshot = await GetCourseFlowAsync();
            using var json = JsonDocument.Parse(snapshot.Json);
            var request = new CourseFlowRequest(json.RootElement, Week: ScheduleDates.Iso(monday));
            var result = await ReadCourseFlowWorkerAsync(request);
            if (version != scheduleRenderVersion || exiting)
                return;
            scheduleState = (result.State ?? json.RootElement).Deserialize<ScheduleState>(CourseService.Json)
                ?? throw new InvalidOperationException("课表数据为空。");
            var settings = scheduleState.Settings;
            var week = ScheduleDates.TermWeek(settings, monday);
            ScheduleTerm.Text = week is > 0 && week <= settings.Weeks ? $"{settings.TermName} · 第 {week} 周" : settings.TermName;
            var days = result.Week?.Days ?? [];
            if (dayView)
                days = days.Where(day => day.Date == ScheduleDates.Iso(date)).ToList();
            var exceptions = (settings.CalendarExceptions ?? []).ToDictionary(rule => rule.Date);
            ScheduleDays.ItemsSource = days.Select(day => exceptions.TryGetValue(day.Date, out var rule) ? day with
            {
                ExceptionLabel = rule.Kind == "off" ? "停课" : $"补{ScheduleDates.DayName(rule.SourceDay)}"
            } : day).ToList();
            ScheduleNext.Text = result.Week?.Next is { } next ? $"接下来 · {next.Date[5..]} {next.Start} · {next.Title}" : "";
            CourseFlowStatus.Text = "";
        }
        catch (OperationCanceledException) when (exiting) { }
        catch (Exception ex)
        {
            if (version != scheduleRenderVersion || exiting)
                return;
            Trace.TraceError("Schedule render failed: {0}", ex.GetType().Name);
            CourseFlowStatus.Text = ex is FormatException ? ex.Message : "无法读取课表，请重试。";
        }
    }

    private async Task CommitScheduleAsync(ScheduleState schedule)
    {
        var data = JsonSerializer.SerializeToElement(schedule, CourseService.Json);
        if (await WriteCourseFlowAsync(data) is not { } saved)
            throw new FormatException("课表已更新，请从最新版本重新编辑。");
        scheduleState = JsonSerializer.Deserialize<ScheduleState>(saved.Json, CourseService.Json);
    }

    private async Task<bool> SaveScheduleDialogAsync(EditDialog dialog, Func<ScheduleState> build, string message)
    {
        if (!dialog.IsEnabled)
            return false;
        dialog.IsEnabled = false;
        try
        {
            await CommitScheduleAsync(build());
            if (exiting)
                return false;
            dialog.Close();
            await RenderScheduleAsync();
            SetStatus(message);
            return true;
        }
        catch (Exception ex)
        {
            if (!exiting)
                dialog.ShowError(ex is FormatException ? ex.Message : "无法保存课表，请重试。");
            return false;
        }
        finally { if (dialog.IsVisible) dialog.IsEnabled = true; }
    }
}
