using System.Globalization;
using System.Windows.Controls.Primitives;

namespace BuctCourseLocal;

public partial class MainWindow
{
    private void Schedule_Add(object sender, RoutedEventArgs e)
    {
        if (scheduleState is null || sender is not Button button)
            return;
        var menu = new ContextMenu { PlacementTarget = button, Placement = PlacementMode.Bottom };
        MenuItem Item(string label, Action action)
        {
            var item = new MenuItem { Header = label };
            item.Click += (_, _) => action();
            return item;
        }
        menu.Items.Add(Item("新建课程", () => EditScheduleCourse(null)));
        menu.Items.Add(Item("新建考试", () => EditScheduleExam(null)));
        menu.IsOpen = true;
    }

    private void Schedule_OpenEntry(object sender, RoutedEventArgs e)
    {
        if (scheduleState is not { } state || sender is not Button { DataContext: ScheduleEventView entry })
            return;
        if (entry.Exam && state.Exams?.FirstOrDefault(x => x.Id == entry.Id) is { } exam)
            EditScheduleExam(exam);
        if (!entry.Exam && state.Courses.FirstOrDefault(x => x.Id == entry.Id) is { } course)
            EditScheduleCourse(course);
    }

    private void Schedule_Manage(object sender, RoutedEventArgs e)
    {
        if (scheduleState is not { } state)
            return;
        var dialog = new EditDialog(this, "全部课程与考试");
        var display = new ListBox { MinHeight = 100, MaxHeight = 320, DisplayMemberPath = "Label" };
        var rows = state.Courses.OrderBy(c => c.Day).ThenBy(c => c.Start)
            .Select(c => (Label: $"{ScheduleDates.DayName(c.Day)} · {c.Start} · {c.Name}", Edit: (Action)(() => EditScheduleCourse(c))))
            .Concat((state.Exams ?? []).OrderBy(x => x.Date).ThenBy(x => x.Start)
                .Select(x => (Label: $"考试 · {x.Date} · {x.Name}", Edit: (Action)(() => EditScheduleExam(x)))))
            .Select(row => new ScheduleChoice(row.Label, row.Edit)).ToList();
        if (rows.Count == 0)
            dialog.AddDescription("暂无课程或考试。");
        else
            dialog.AddField("选择项目", display);
        display.ItemsSource = rows;
        dialog.AddAction("关闭", dialog.Close, cancel: true);
        if (rows.Count > 0)
            dialog.AddAction("编辑", () =>
            {
                if (display.SelectedItem is not ScheduleChoice choice)
                {
                    dialog.ShowError("请选择项目。");
                    return;
                }
                dialog.Close();
                choice.Open();
            }, primary: true);
        ShowDialog(dialog);
    }

    private sealed record ScheduleChoice(string Label, Action Open);

    private void EditScheduleCourse(ScheduleCourse? existing)
    {
        if (scheduleState is not { } state)
            return;
        var dialog = new EditDialog(this, existing is null ? "新建课程" : "编辑课程");
        var name = Field("课程名称", existing?.Name);
        var location = Field("上课地点", existing?.Location);
        var teacher = Field("教师", existing?.Teacher);
        var defaultDate = scheduleDayView ? scheduleDate ?? ScheduleDates.Today : ScheduleDates.Today;
        var day = Choices("星期", Enumerable.Range(1, 7).Select(ScheduleDates.DayName), (existing?.Day ?? ScheduleDates.DayNumber(defaultDate)) - 1);
        var basis = Choices("时间方式", ["按钟点", "按节次"], existing is { PeriodStart: > 0, PeriodEnd: > 0 } ? 1 : 0);
        var start = Field("开始时间或起始节次", basis.SelectedIndex == 1 ? existing!.PeriodStart.ToString() : existing?.Start ?? state.Settings.Periods[0].Start);
        var end = Field("结束时间或结束节次", basis.SelectedIndex == 1 ? existing!.PeriodEnd.ToString() : existing?.End ?? state.Settings.Periods[0].End);
        basis.SelectionChanged += (_, _) =>
        {
            start.Text = basis.SelectedIndex == 1 ? (existing?.PeriodStart is > 0 ? existing.PeriodStart.ToString() : "1") : existing?.Start ?? state.Settings.Periods[0].Start;
            end.Text = basis.SelectedIndex == 1 ? (existing?.PeriodEnd is > 0 ? existing.PeriodEnd.ToString() : "1") : existing?.End ?? state.Settings.Periods[0].End;
        };
        var week = Field("教学周（例如 1-16）", existing is null ? $"1-{state.Settings.Weeks}" : $"{existing.WeekStart}-{existing.WeekEnd}");
        var parity = Choices("上课频率", ["每周", "单周", "双周"], existing?.Parity switch { "odd" => 1, "even" => 2, _ => 0 });
        var skip = Field("单次停课日（YYYY-MM-DD，逗号分隔）", string.Join(",", existing?.ExcludeDates ?? []));
        var note = Field("备注", existing?.Note);
        dialog.AddAction("取消", dialog.Close, cancel: true);
        if (existing is not null)
            dialog.AddAction("删除", async () =>
            {
                if (ConfirmScheduleDelete(dialog, existing.Name))
                    await SaveScheduleDialogAsync(dialog, () => state with { Courses = state.Courses.Where(c => c.Id != existing.Id).ToList() }, "已删除课程。");
            });
        dialog.AddAction("保存", async () => await SaveScheduleDialogAsync(dialog, () =>
        {
            var (first, last) = ReadRange(week.Text, "教学周");
            var periodStart = 0;
            var periodEnd = 0;
            var timeStart = start.Text.Trim();
            var timeEnd = end.Text.Trim();
            if (basis.SelectedIndex == 1)
            {
                (periodStart, periodEnd) = ReadRange($"{start.Text}-{end.Text}", "节次");
                if (periodEnd > state.Settings.Periods.Count)
                    throw new FormatException("节次超出作息表。");
                timeStart = state.Settings.Periods[periodStart - 1].Start;
                timeEnd = state.Settings.Periods[periodEnd - 1].End;
            }
            var exclusions = skip.Text.Split([',', '，', '\n', '\r'], StringSplitOptions.TrimEntries | StringSplitOptions.RemoveEmptyEntries)
                .Select(value => ScheduleDates.Iso(ScheduleDates.Parse(value))).Distinct().ToList();
            var item = new ScheduleCourse(existing?.Id ?? Guid.NewGuid().ToString("N"), name.Text.Trim(), teacher.Text.Trim(), location.Text.Trim(),
                day.SelectedIndex + 1, ReadClock(timeStart), ReadClock(timeEnd), first, last, new[] { "all", "odd", "even" }[parity.SelectedIndex],
                existing?.Color ?? "mint", note.Text.Trim(), periodStart, periodEnd, exclusions);
            return state with { Courses = state.Courses.Where(x => x.Id != item.Id).Append(item).ToList() };
        }, "已保存课程。"), primary: true);
        ShowDialog(dialog);

        TextBox Field(string label, string? value)
        {
            var box = new TextBox { Text = value ?? "" };
            dialog.AddField(label, box);
            return box;
        }
        ComboBox Choices(string label, IEnumerable<string> items, int selected)
        {
            var box = new ComboBox { ItemsSource = items.ToList(), SelectedIndex = selected, Padding = new Thickness(6, 4, 6, 4) };
            dialog.AddField(label, box);
            return box;
        }
    }

    private void EditScheduleExam(ScheduleExam? existing)
    {
        if (scheduleState is not { } state)
            return;
        var dialog = new EditDialog(this, existing is null ? "新建考试" : "编辑考试");
        var name = Field("考试名称", existing?.Name);
        var defaultDate = scheduleDayView ? scheduleDate ?? ScheduleDates.Today : ScheduleDates.Today;
        var date = new DatePicker { SelectedDate = (existing is null ? defaultDate : ScheduleDates.Parse(existing.Date)).ToDateTime(TimeOnly.MinValue) };
        dialog.AddField("考试日期", date);
        var start = Field("开始时间（HH:mm）", existing?.Start ?? "09:00");
        var end = Field("结束时间（HH:mm）", existing?.End ?? "11:00");
        var location = Field("考场", existing?.Location);
        var note = Field("备注", existing?.Note);
        dialog.AddAction("取消", dialog.Close, cancel: true);
        if (existing is not null)
            dialog.AddAction("删除", async () =>
            {
                if (ConfirmScheduleDelete(dialog, existing.Name))
                    await SaveScheduleDialogAsync(dialog, () => state with { Exams = (state.Exams ?? []).Where(c => c.Id != existing.Id).ToList() }, "已删除考试。");
            });
        dialog.AddAction("保存", async () => await SaveScheduleDialogAsync(dialog, () =>
        {
            if (date.SelectedDate is not { } selected)
                throw new FormatException("请选择考试日期。");
            var item = new ScheduleExam(existing?.Id ?? Guid.NewGuid().ToString("N"), name.Text.Trim(), ScheduleDates.Iso(DateOnly.FromDateTime(selected)),
                ReadClock(start.Text), ReadClock(end.Text), location.Text.Trim(), note.Text.Trim());
            return state with { Exams = (state.Exams ?? []).Where(c => c.Id != item.Id).Append(item).ToList() };
        }, "已保存考试。"), primary: true);
        ShowDialog(dialog);

        TextBox Field(string label, string? value)
        {
            var box = new TextBox { Text = value ?? "" };
            dialog.AddField(label, box);
            return box;
        }
    }

    private void Schedule_Settings(object sender, RoutedEventArgs e)
    {
        if (scheduleState is not { } state)
            return;
        var settings = state.Settings;
        var dialog = new EditDialog(this, "学期与作息");
        var name = new TextBox { Text = settings.TermName };
        var begin = new DatePicker { SelectedDate = ScheduleDates.Parse(settings.TermStart).ToDateTime(TimeOnly.MinValue) };
        var weeks = new TextBox { Text = settings.Weeks.ToString() };
        var reminder = new CheckBox { Content = "启用课程与考试提醒", IsChecked = settings.Reminder, Margin = new Thickness(1, 10, 0, 0) };
        var minutes = new TextBox { Text = settings.Minutes.ToString() };
        var periods = new TextBox { Text = string.Join("\n", settings.Periods.Select(p => $"{p.Start}-{p.End}")), AcceptsReturn = true, Height = 105,
            VerticalScrollBarVisibility = ScrollBarVisibility.Auto };
        var calendar = new TextBox { Text = string.Join("\n", (settings.CalendarExceptions ?? []).Select(rule =>
            $"{rule.Date},{(rule.Kind == "off" ? "停课" : $"补{ScheduleDates.DayName(rule.SourceDay)}")},{rule.Note}")), AcceptsReturn = true,
            Height = 84, VerticalScrollBarVisibility = ScrollBarVisibility.Auto };
        dialog.AddField("学期名称", name);
        dialog.AddField("第一周周一", begin);
        dialog.AddField("学期周数", weeks);
        var test = new Button { Content = "测试课前提醒" };
        test.Click += (_, _) => ShowCourseFlowNotice("课表通知测试", "课程与考试提醒已接入托盘。");
        dialog.AddOption(test);
        dialog.AddField("提醒设置", reminder);
        dialog.AddField("提前提醒（分钟）", minutes);
        dialog.AddField("每日作息（每行一节，例如 08:00-08:45）", periods);
        dialog.AddField("校历调整（例如 2026-12-25,停课 或 2026-12-26,补周一）", calendar);
        dialog.AddAction("取消", dialog.Close, cancel: true);
        dialog.AddAction("保存", async () => await SaveScheduleDialogAsync(dialog, () =>
        {
            if (begin.SelectedDate is not { } first || !int.TryParse(weeks.Text, out var count) || !int.TryParse(minutes.Text, out var lead))
                throw new FormatException("请填写日期、周数和提醒时间。");
            var rows = Lines(periods.Text).Select(line => line.Split('-', StringSplitOptions.TrimEntries)).Select(parts =>
                parts.Length == 2 ? new SchedulePeriod(ReadClock(parts[0]), ReadClock(parts[1])) : throw new FormatException("作息需要起止时刻。")).ToList();
            var rules = Lines(calendar.Text).Select(ParseScheduleException).ToList();
            var courses = state.Courses.Select(course => WithPeriods(course, rows)).ToList();
            return state with { Settings = new ScheduleSettings(name.Text.Trim(), ScheduleDates.Iso(DateOnly.FromDateTime(first)), count,
                reminder.IsChecked == true, lead, rows, rules), Courses = courses };
        }, "已保存学期与作息。"), primary: true);
        ShowDialog(dialog);
    }

    private static ScheduleException ParseScheduleException(string line)
    {
        var parts = line.Replace('，', ',').Split(',', 3, StringSplitOptions.TrimEntries);
        if (parts.Length < 2)
            throw new FormatException("请填写校历调整的日期和类型。");
        var day = Enumerable.Range(1, 7).FirstOrDefault(i => parts[1] == $"补{ScheduleDates.DayName(i)}");
        if (parts[1] != "停课" && day == 0)
            throw new FormatException("校历调整应为停课或补周一至周日。");
        return new ScheduleException(ScheduleDates.Iso(ScheduleDates.Parse(parts[0])), day == 0 ? "off" : "makeup", day, parts.Length == 3 ? parts[2] : "");
    }

    private static ScheduleCourse WithPeriods(ScheduleCourse course, IReadOnlyList<SchedulePeriod> periods)
    {
        if (course.PeriodStart <= 0 && course.PeriodEnd <= 0)
            return course;
        if (course.PeriodStart < 1 || course.PeriodEnd < course.PeriodStart || course.PeriodEnd > periods.Count)
            throw new FormatException($"「{course.Name}」使用的节次超出新作息表。");
        return course with { Start = periods[course.PeriodStart - 1].Start, End = periods[course.PeriodEnd - 1].End };
    }

    private static (int Start, int End) ReadRange(string raw, string name)
    {
        var items = raw.Split('-', StringSplitOptions.TrimEntries);
        if (items.Length != 2 || !int.TryParse(items[0], out var first) || !int.TryParse(items[1], out var last) || first < 1 || last < first)
            throw new FormatException($"请输入有效的{name}范围。");
        return (first, last);
    }

    private static string ReadClock(string raw) => TimeOnly.TryParseExact(raw.Trim(), ["H:mm", "HH:mm"], CultureInfo.InvariantCulture, DateTimeStyles.None, out var time)
        ? time.ToString("HH:mm", CultureInfo.InvariantCulture) : throw new FormatException("时间请使用 HH:mm 格式。");

    private static string[] Lines(string text) => text.Split(['\r', '\n'], StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries);

    private static bool ConfirmScheduleDelete(Window owner, string name) =>
        MessageBox.Show(owner, $"删除「{name}」？", "删除课表项目", MessageBoxButton.YesNo) == MessageBoxResult.Yes;
}
