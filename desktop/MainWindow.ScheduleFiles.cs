using System.Diagnostics;
using System.Globalization;
using System.Text;
using System.Text.Json;
using Microsoft.Win32;
using Ical.Net.CalendarComponents;
using Ical.Net.DataTypes;
using Ical.Net.Serialization;

namespace BuctCourseLocal;

public partial class MainWindow
{
    private bool scheduleFileBusy;

    private async void Schedule_Import(object sender, RoutedEventArgs e)
    {
        if (scheduleState is not { } state || scheduleFileBusy)
            return;
        var picker = new OpenFileDialog
        {
            Title = "选择课表或作息文件",
            Filter = "课表与作息 (*.csv;*.xlsx;*.xls;*.ics;*.pdf;*.png;*.jpg;*.jpeg)|*.csv;*.xlsx;*.xls;*.ics;*.pdf;*.png;*.jpg;*.jpeg"
        };
        if (picker.ShowDialog(this) != true)
            return;
        scheduleFileBusy = true;
        CourseFlowStatus.Text = "正在读取所选文件…";
        try
        {
            var result = await Task.Run(() => ScheduleFileImport.Read(picker.FileName, state.Settings), quit.Token);
            if (exiting)
                return;
            if (result.Courses.Count == 0 && result.Periods is not { Count: > 0 } && result.Exams is not { Count: > 0 })
                throw new FormatException("没有可导入的课程。请检查表头、识别结果或学期日期。");
            CourseFlowStatus.Text = "";
            ShowScheduleImport(state, result);
        }
        catch (Exception ex)
        {
            if (!exiting)
            {
                Trace.TraceInformation("Schedule import failed: {0}", ex.GetType().Name);
                CourseFlowStatus.Text = ex is FormatException ? ex.Message : "无法读取选中的课表文件。";
            }
        }
        finally { scheduleFileBusy = false; }
    }

    private void ShowScheduleImport(ScheduleState state, ScheduleImportData result)
    {
        var dialog = new EditDialog(this, "核对导入内容");
        dialog.AddAction("取消", dialog.Close, cancel: true);
        if (result.Periods is { } periods)
        {
            dialog.AddDescription($"将更新每日作息，共 {periods.Count} 节。");
            dialog.AddField("作息预览", new ListBox { ItemsSource = periods.Select((p, i) => $"第 {i + 1} 节 · {p.Start}–{p.End}"), MaxHeight = 280 });
            dialog.AddAction("导入", async () => await SaveScheduleDialogAsync(dialog, () => state with
            {
                Settings = state.Settings with { Periods = periods }, Courses = state.Courses.Select(c => WithPeriods(c, periods)).ToList()
            }, "已导入作息表。"), primary: true);
        }
        else
        {
            var courses = result.Courses.DistinctBy(c => c.Id).ToList();
            var exams = (result.Exams ?? []).DistinctBy(e => e.Id).ToList();
            dialog.AddDescription($"可导入 {courses.Count} 条课程、{exams.Count} 项考试。" +
                (result.Skipped > 0 ? $"已跳过 {result.Skipped} 条无法识别或超出学期的记录。" : ""));
            dialog.AddField("导入预览", new ListBox { ItemsSource = courses.Select(c =>
                $"{ScheduleDates.DayName(c.Day)} · {c.Start}–{c.End} · {c.Name} · {c.Location}")
                .Concat(exams.Select(e => $"考试 · {e.Date} {e.Start} · {e.Name}")), MaxHeight = 300 });
            dialog.AddAction("导入", async () => await SaveScheduleDialogAsync(dialog, () => state with
            {
                Courses = courses.Concat(state.Courses).DistinctBy(c => c.Id).ToList(),
                Exams = exams.Concat(state.Exams ?? []).DistinctBy(e => e.Id).ToList()
            }, $"已导入 {courses.Count} 条课程、{exams.Count} 项考试。"), primary: true);
        }
        ShowDialog(dialog);
    }

    private async void Schedule_Export(object sender, RoutedEventArgs e)
    {
        if (scheduleState is not { } state || scheduleFileBusy)
            return;
        var picker = new SaveFileDialog { Title = "导出课表", FileName = "课表", Filter = "日历 (*.ics)|*.ics|课程表格 (*.csv)|*.csv|作息表格 (*.csv)|*.csv" };
        if (picker.ShowDialog(this) != true)
            return;
        scheduleFileBusy = true;
        try
        {
            string content;
            if (picker.FilterIndex == 1)
            {
                var raw = JsonSerializer.SerializeToElement(state, CourseService.Json);
                var result = await ReadCourseFlowWorkerAsync(new CourseFlowRequest(raw, Export: true));
                content = FormatCalendar(result.Events ?? []);
            }
            else if (picker.FilterIndex == 2)
                content = Csv(["课程名称", "星期", "开始时间", "结束时间", "地点", "教师", "开始周", "结束周", "单双周", "备注"],
                    state.Courses.Select(c => new[] { c.Name, c.Day.ToString(), c.Start, c.End, c.Location, c.Teacher, c.WeekStart.ToString(),
                        c.WeekEnd.ToString(), c.Parity, c.Note }));
            else
                content = Csv(["节次", "开始时间", "结束时间"], state.Settings.Periods.Select((p, i) => new[] { (i + 1).ToString(), p.Start, p.End }));
            await File.WriteAllTextAsync(picker.FileName, content, new UTF8Encoding(picker.FilterIndex != 1), quit.Token);
            if (!exiting)
                SetStatus("课表已导出。");
        }
        catch (Exception ex)
        {
            if (!exiting)
            {
                Trace.TraceInformation("Schedule export failed: {0}", ex.GetType().Name);
                CourseFlowStatus.Text = ex is FormatException ? ex.Message : "无法导出课表，请检查保存位置。";
            }
        }
        finally { scheduleFileBusy = false; }
    }

    private static string Csv(string[] header, IEnumerable<string[]> rows) => string.Join("\r\n", rows.Prepend(header)
        .Select(row => string.Join(",", row.Select(value => $"\"{value.Replace("\"", "\"\"")}\"")))) + "\r\n";

    private static string FormatCalendar(IEnumerable<ScheduleEventView> entries)
    {
        var calendar = new Ical.Net.Calendar();
        foreach (var entry in entries)
        {
            DateTime Utc(string clock) => new DateTimeOffset(DateTime.ParseExact(entry.Date + " " + clock, "yyyy-MM-dd HH:mm",
                CultureInfo.InvariantCulture), CourseTask.BeijingOffset).UtcDateTime;
            calendar.Events.Add(new CalendarEvent
            {
                Uid = $"{(entry.Exam ? "exam" : "course")}-{entry.Id}-{entry.Date}@buct-course-local",
                Summary = entry.Title, Location = entry.Location, Description = entry.Teacher,
                DtStart = new CalDateTime(Utc(entry.Start), "UTC"), DtEnd = new CalDateTime(Utc(entry.End), "UTC")
            });
        }
        return new CalendarSerializer().SerializeToString(calendar) ?? throw new InvalidOperationException("无法生成 ICS 文件。");
    }
}
