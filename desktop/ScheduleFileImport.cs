using System.Globalization;
using System.Security.Cryptography;
using System.Text;
using System.Text.RegularExpressions;
using ExcelDataReader;
using Ical.Net;
using Ical.Net.CalendarComponents;
using Ical.Net.DataTypes;
using Microsoft.VisualBasic.FileIO;

namespace BuctCourseLocal;

internal record ScheduleImportData(List<ScheduleCourse> Courses, List<SchedulePeriod>? Periods = null, int Skipped = 0, List<ScheduleExam>? Exams = null);

internal static partial class ScheduleFileImport
{
    internal static ScheduleImportData Read(string path, ScheduleSettings settings)
    {
        if (new FileInfo(path).Length > 25 * 1024 * 1024)
            throw new FormatException("导入文件不能超过 25 MB。");
        return Path.GetExtension(path).ToLowerInvariant() switch
        {
            ".ics" => ReadCalendar(path, settings),
            ".csv" or ".xlsx" or ".xls" => ReadRows(ReadTable(path), settings),
            _ => ReadDocument(path, settings)
        };
    }

    private static ScheduleImportData ReadDocument(string path, ScheduleSettings settings)
    {
        var document = ScheduleDocumentReader.Read(path);
        var layout = ScheduleSchoolLayout.Read(document.Pages, settings);
        return layout.Courses.Count > 0 || layout.Periods is { Count: > 0 } ? layout : ReadRecognized(document.Text, settings);
    }

    private static List<string[]> ReadTable(string path)
    {
        var rows = new List<string[]>();
        if (Path.GetExtension(path).Equals(".csv", StringComparison.OrdinalIgnoreCase))
        {
            Encoding.RegisterProvider(CodePagesEncodingProvider.Instance);
            var bytes = File.ReadAllBytes(path);
            string text;
            try { text = new UTF8Encoding(false, true).GetString(bytes); }
            catch (DecoderFallbackException) { text = Encoding.GetEncoding(54936).GetString(bytes); }
            using var reader = new TextFieldParser(new StringReader(text.TrimStart('\uFEFF')));
            reader.SetDelimiters(",");
            while (!reader.EndOfData)
                rows.Add(reader.ReadFields() ?? []);
        }
        else
        {
            Encoding.RegisterProvider(CodePagesEncodingProvider.Instance);
            using var stream = File.OpenRead(path);
            using var reader = ExcelReaderFactory.CreateReader(stream);
            do
            {
                while (reader.Read())
                    rows.Add(Enumerable.Range(0, reader.FieldCount).Select(i => Cell(reader.GetValue(i))).ToArray());
            } while (rows.Count == 0 && reader.NextResult());
        }
        if (rows.Count > 1200)
            throw new FormatException("表格不能超过 1200 行。");
        return rows;
    }

    private static string Cell(object? value) => value switch
    {
        DateTime time => time.ToString("HH:mm", CultureInfo.InvariantCulture),
        TimeSpan time => time.ToString(@"hh\:mm", CultureInfo.InvariantCulture),
        double number when number is > 0 and < 1 => TimeSpan.FromMinutes(Math.Round(number * 1440)).ToString(@"hh\:mm", CultureInfo.InvariantCulture),
        _ => Convert.ToString(value, CultureInfo.InvariantCulture) ?? ""
    };

    private static ScheduleImportData ReadRows(List<string[]> rows, ScheduleSettings settings)
    {
        rows = rows.Where(row => row.Any(value => !string.IsNullOrWhiteSpace(value))).ToList();
        if (rows.Count < 2)
            throw new FormatException("文件里没有可导入的记录。");
        var headers = rows[0].Select(x => x.Trim().TrimStart('\uFEFF')).ToList();
        int Column(params string[] choices) => headers.FindIndex(value => choices.Contains(value, StringComparer.OrdinalIgnoreCase));
        var name = Column("课程名称", "课程", "name");
        var day = Column("星期", "day");
        var start = Column("开始时间", "开始", "start");
        var end = Column("结束时间", "结束", "end");
        if (name < 0 && start >= 0 && end >= 0 && Column("节次", "period") >= 0)
            return new ScheduleImportData([], rows.Skip(1).Select(row => new SchedulePeriod(Clock(Value(row, start)), Clock(Value(row, end)))).ToList());
        if (name < 0 || day < 0 || start < 0 || end < 0)
            throw new FormatException("未找到课程名称、星期、开始时间和结束时间表头。");
        var location = Column("地点", "上课地点", "location");
        var teacher = Column("教师", "teacher");
        var first = Column("开始周", "week_start");
        var last = Column("结束周", "week_end");
        var parity = Column("单双周", "parity");
        var notes = Column("备注", "note");
        var parsed = ParseRows(rows.Skip(1), row => MakeCourse(Value(row, name), Weekday(Value(row, day)), Clock(Value(row, start)),
            Clock(Value(row, end)), Value(row, location), Value(row, teacher), Number(Value(row, first), 1),
            Number(Value(row, last), settings.Weeks), Parity(Value(row, parity)), Value(row, notes)), settings);
        return parsed;
    }

    private static ScheduleImportData ReadCalendar(string path, ScheduleSettings settings)
    {
        var calendar = Ical.Net.Calendar.Load(File.ReadAllText(path)) ?? throw new FormatException("无法读取 ICS 日历。");
        var start = ScheduleDates.Parse(settings.TermStart);
        var end = start.AddDays(settings.Weeks * 7);
        var since = new CalDateTime(start.ToDateTime(TimeOnly.MinValue).AddDays(-1), "Asia/Shanghai");
        var until = new CalDateTime(end.ToDateTime(TimeOnly.MinValue).AddDays(1), "Asia/Shanghai");
        var entries = new List<ScheduleCourse>();
        var exams = new List<ScheduleExam>();
        var skipped = 0;
        foreach (var occurrence in calendar.GetOccurrences(since).TakeWhileBefore(until))
        {
            if (entries.Count + exams.Count + skipped >= 1200)
                throw new FormatException("当前学期的 ICS 事件超过导入上限。");
            if (occurrence.Source is not CalendarEvent item || occurrence.Period.StartTime is not { HasTime: true } begins ||
                occurrence.Period.EffectiveEndTime is not { } finishes)
            {
                skipped++;
                continue;
            }
            var a = Local(begins);
            var b = Local(finishes);
            var date = DateOnly.FromDateTime(a);
            if (date < start || date >= end || a.Date != b.Date || b <= a || string.IsNullOrWhiteSpace(item.Summary))
            {
                skipped++;
                continue;
            }
            var week = ScheduleDates.TermWeek(settings, date);
            if (item.Uid is { } uid && uid.StartsWith("exam-", StringComparison.Ordinal) && uid.EndsWith("@buct-course-local", StringComparison.Ordinal))
            {
                var name = item.Summary.StartsWith("考试 · ", StringComparison.Ordinal) ? item.Summary[5..] : item.Summary;
                var id = "import-" + Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(uid)));
                exams.Add(new ScheduleExam(id, name, ScheduleDates.Iso(date), a.ToString("HH:mm"), b.ToString("HH:mm"), item.Location ?? "", ""));
            }
            else
                entries.Add(MakeCourse(item.Summary, ScheduleDates.DayNumber(date), a.ToString("HH:mm"), b.ToString("HH:mm"),
                    item.Location ?? "", "", week, week, "all", item.Description ?? ""));
        }
        return new ScheduleImportData(entries, Skipped: skipped, Exams: exams);

        static DateTime Local(CalDateTime value) => string.IsNullOrEmpty(value.TzId) ? value.Value :
            value.AsUtc.Add(CourseTask.BeijingOffset);
    }

    private static ScheduleImportData ReadRecognized(string text, ScheduleSettings settings)
    {
        var lines = text.Split(['\r', '\n'], StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries);
        if (lines.FirstOrDefault(line => line.Contains("课程名称", StringComparison.Ordinal)) is { } header && header.Contains(','))
        {
            using var reader = new TextFieldParser(new StringReader(string.Join("\n", lines.SkipWhile(x => x != header))));
            reader.SetDelimiters(",");
            var rows = new List<string[]>();
            while (!reader.EndOfData)
                rows.Add(reader.ReadFields() ?? []);
            return ReadRows(rows, settings);
        }
        var matches = lines.Select(line => RecognizedLine().Match(line)).Where(match => match.Success);
        return ParseRows(matches, match => MakeCourse(match.Groups["title"].Value.Trim(), Weekday(match.Groups["day"].Value),
            Clock(match.Groups["start"].Value), Clock(match.Groups["end"].Value), match.Groups["place"].Value.Trim(), "", 1,
            settings.Weeks, "all", ""), settings);
    }

    [GeneratedRegex(@"^(?<title>.+?)\s+(?<day>(?:星期|周)[一二三四五六日天]|[1-7])\s+(?<start>\d{1,2}[:：]\d{2})\s*[-–—~至]\s*(?<end>\d{1,2}[:：]\d{2})\s*(?<place>.*)$")]
    private static partial Regex RecognizedLine();

    private static ScheduleImportData ParseRows<T>(IEnumerable<T> rows, Func<T, ScheduleCourse> create, ScheduleSettings settings)
    {
        var valid = new List<ScheduleCourse>();
        var skipped = 0;
        foreach (var row in rows)
        {
            try
            {
                var course = create(row);
                if (course.Name.Length == 0 || string.CompareOrdinal(course.Start, course.End) >= 0 || course.WeekStart < 1 ||
                    course.WeekEnd < course.WeekStart || course.WeekEnd > settings.Weeks)
                    throw new FormatException();
                valid.Add(course);
            }
            catch (FormatException) { skipped++; }
        }
        return new ScheduleImportData(valid, Skipped: skipped);
    }

    internal static ScheduleCourse MakeCourse(string name, int day, string start, string end, string location, string teacher,
        int first, int last, string parity, string note)
    {
        name = name.Trim();
        location = string.IsNullOrWhiteSpace(location) ? "地点待定" : location.Trim();
        var key = string.Join('\u001F', name, day.ToString(CultureInfo.InvariantCulture), start, end, location, first.ToString(), last.ToString(), parity);
        var id = "import-" + Convert.ToHexString(SHA256.HashData(Encoding.UTF8.GetBytes(key)));
        return new ScheduleCourse(id, name, teacher.Trim(), location, day, start, end, first, last, parity, "mint", note.Trim());
    }

    private static string Value(string[] row, int index) => index >= 0 && index < row.Length ? row[index].Trim() : "";
    private static int Number(string value, int fallback) => value.Length == 0 ? fallback : int.TryParse(value, out var number) ? number : throw new FormatException();
    private static string Clock(string value) => TimeOnly.TryParseExact(value.Trim().Replace('：', ':'), ["H:mm", "HH:mm", "HH:mm:ss"],
        CultureInfo.InvariantCulture, DateTimeStyles.None, out var time) ? time.ToString("HH:mm", CultureInfo.InvariantCulture) : throw new FormatException("时刻格式应为 HH:mm。");
    private static int Weekday(string value)
    {
        var day = value.Trim().Replace("星期", "周").Replace('天', '日');
        if (int.TryParse(day, out var index) && index is >= 1 and <= 7)
            return index;
        index = Enumerable.Range(1, 7).FirstOrDefault(i => ScheduleDates.DayName(i) == day);
        return index > 0 ? index : throw new FormatException("星期应为 1–7 或周一至周日。");
    }
    private static string Parity(string value) => value.ToLowerInvariant() switch
    {
        "" or "all" or "每周" => "all", "odd" or "单周" => "odd", "even" or "双周" => "even", _ => throw new FormatException("单双周格式无效。")
    };
}
