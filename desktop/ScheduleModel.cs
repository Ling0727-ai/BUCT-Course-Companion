using System.Globalization;
using System.Text.Json.Serialization;

namespace BuctCourseLocal;

internal record ScheduleState(long Revision, List<ScheduleCourse> Courses, ScheduleSettings Settings, List<ScheduleExam>? Exams);
internal record ScheduleCourse(string Id, string Name, string Teacher, string Location, int Day, string Start, string End,
    [property: JsonPropertyName("week_start")] int WeekStart, [property: JsonPropertyName("week_end")] int WeekEnd,
    string Parity, string Color, string Note,
    [property: JsonPropertyName("period_start")] int PeriodStart = 0, [property: JsonPropertyName("period_end")] int PeriodEnd = 0,
    [property: JsonPropertyName("exclude_dates")] List<string>? ExcludeDates = null);
internal record ScheduleExam(string Id, string Name, string Date, string Start, string End, string Location, string Note);
internal record SchedulePeriod(string Start, string End);
internal record ScheduleException(string Date, string Kind, [property: JsonPropertyName("source_day")] int SourceDay, string Note);
internal record ScheduleSettings([property: JsonPropertyName("term_name")] string TermName,
    [property: JsonPropertyName("term_start")] string TermStart, int Weeks, bool Reminder, int Minutes, List<SchedulePeriod> Periods,
    [property: JsonPropertyName("calendar_exceptions")] List<ScheduleException>? CalendarExceptions);
internal record ScheduleWeekView(List<ScheduleDayView> Days, ScheduleEventView? Next);
internal record DashboardSchedule([property: JsonPropertyName("term_name")] string TermName,
    [property: JsonPropertyName("term_week")] int TermWeek, ScheduleDayView Today, List<ScheduleEventView> Exams);
internal record ScheduleDayView(string Date, bool Today, List<ScheduleEventView> Entries)
{
    public string DayLabel => ScheduleDates.DayName(ScheduleDates.DayNumber(ScheduleDates.Parse(Date)));
    public string ShortDate => ScheduleDates.Parse(Date).ToString("MM/dd", CultureInfo.InvariantCulture);
    public string ExceptionLabel { get; init; } = "";
}
internal record ScheduleEventView(string Id, string Name, string Date, string Start, string End, string Location, string Teacher, bool Exam, string Color = "")
{
    public string Title => Exam ? $"考试 · {Name}" : Name;
    public string TimeLabel => $"{Start}–{End}";
    public string Metadata => string.Join(" · ", new[] { Location, Teacher }.Where(value => !string.IsNullOrWhiteSpace(value)));
}

internal static class ScheduleDates
{
    private static readonly string[] Names = ["周一", "周二", "周三", "周四", "周五", "周六", "周日"];
    internal static string DayName(int day) => Names[day - 1];
    internal static int DayNumber(DateOnly date) => ((int)date.DayOfWeek + 6) % 7 + 1;
    internal static DateOnly Monday(DateOnly date) => date.AddDays(1 - DayNumber(date));
    internal static DateOnly Today => DateOnly.FromDateTime(CourseTask.BeijingTime(DateTimeOffset.UtcNow.ToUnixTimeSeconds()).DateTime);
    internal static DateOnly Parse(string date) => DateOnly.ParseExact(date, "yyyy-MM-dd", CultureInfo.InvariantCulture);
    internal static string Iso(DateOnly date) => date.ToString("yyyy-MM-dd", CultureInfo.InvariantCulture);
    internal static int TermWeek(ScheduleSettings settings, DateOnly date)
    {
        var elapsed = date.DayNumber - Parse(settings.TermStart).DayNumber;
        return elapsed < 0 ? 0 : elapsed / 7 + 1;
    }
}
