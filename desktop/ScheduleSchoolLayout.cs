using System.Text;
using System.Text.RegularExpressions;

namespace BuctCourseLocal;

// Native adaptation of course-schedule/web/src/importing/school_layout.ts.
internal static partial class ScheduleSchoolLayout
{
    [GeneratedRegex(@"^(?:星期|周)([一二三四五六日天])$")]
    private static partial Regex DayHeading();
    [GeneratedRegex(@"^[（(](\d{1,2})(?:[-~至—–](\d{1,2}))?节[）)](.*)")]
    private static partial Regex LessonHeading();
    [GeneratedRegex(@"(\d{1,2})(?:[-~至—–](\d{1,2}))?周?(?:[(（]([单双])(?:周)?[)）])?")]
    private static partial Regex WeekRange();
    [GeneratedRegex(@"(?:场地|教室|地点)[:：]([^/／]+)")]
    private static partial Regex LocationValue();
    [GeneratedRegex(@"(?:教师|老师)[:：]([^/／]+)")]
    private static partial Regex TeacherValue();
    [GeneratedRegex(@"[/:：;；]|学分|学时|教学班|^\d+$")]
    private static partial Regex MetadataLine();
    [GeneratedRegex(@"^(?:第)?(\d{1,2})(?:节|堂)?[、:：.]?(\d{1,2}[:：]\d{2})[-–—~至](\d{1,2}[:：]\d{2})$")]
    private static partial Regex PeriodLine();

    private sealed record Heading(int Day, double Center, double Bottom);
    private sealed record Lesson(int Line, int NameLine, string Name, int First, int Last, string Body);

    internal static ScheduleImportData Read(IReadOnlyList<ScheduleTextPage> pages, ScheduleSettings settings)
    {
        var columns = new Dictionary<int, List<string>>();
        List<Heading> headings = [];
        foreach (var page in pages.Where(page => page.Width > 0))
        {
            var pageHeadings = Rows(page.Boxes).SelectMany(Segments).Select(box => (Match: DayHeading().Match(box.Text), Box: box))
                .Where(hit => hit.Match.Success).Select(hit => new Heading(
                    "一二三四五六日".IndexOf(hit.Match.Groups[1].Value.Replace('天', '日'), StringComparison.Ordinal) + 1,
                    hit.Box.CenterX / page.Width, hit.Box.Y + hit.Box.Height)).OrderBy(h => h.Center).DistinctBy(h => h.Day).ToList();
            if (pageHeadings.Count >= 3)
                headings = pageHeadings;
            if (headings.Count < 3)
                continue;
            var bottom = pageHeadings.Count >= 3 ? pageHeadings.Max(h => h.Bottom) : 0;
            var spacing = headings.Zip(headings.Skip(1), (a, b) => (b.Center - a.Center) / Math.Max(1, b.Day - a.Day)).Where(d => d > 0).Order().ToArray();
            var columnWidth = spacing.Length > 0 ? spacing[spacing.Length / 2] : 1.0 / 7;
            foreach (var heading in headings)
            {
                var words = page.Boxes.Where(box => box.Y > bottom && Math.Abs(box.CenterX / page.Width - heading.Center) < columnWidth / 2 &&
                    box.Width / page.Width < columnWidth * 1.25);
                if (!columns.TryGetValue(heading.Day, out var lines))
                    columns[heading.Day] = lines = [];
                lines.AddRange(Rows(words).Select(row => Tight(string.Concat(row.Select(box => box.Text)))));
            }
        }

        var courses = new List<ScheduleCourse>();
        var skipped = 0;
        foreach (var (weekday, lines) in columns)
        {
            var markers = Lessons(lines);
            foreach (var (lesson, index) in markers.Select((lesson, index) => (lesson, index)))
            {
                if (lesson.First < 1 || lesson.Last < lesson.First || lesson.Last > settings.Periods.Count)
                {
                    skipped++;
                    continue;
                }
                var next = index + 1 < markers.Count ? markers[index + 1].NameLine : lines.Count;
                var content = lesson.Body + string.Concat(lines.Skip(lesson.Line + 1).Take(Math.Max(0, next - lesson.Line - 1)));
                var weeks = content.Split(['/', '／'], 2)[0];
                var matches = WeekRange().Matches(weeks);
                if (matches.Count == 0)
                    skipped++;
                foreach (Match match in matches)
                {
                    var first = int.Parse(match.Groups[1].Value);
                    var last = match.Groups[2].Success ? int.Parse(match.Groups[2].Value) : first;
                    if (first < 1 || last < first || last > settings.Weeks)
                    {
                        skipped++;
                        continue;
                    }
                    var parity = match.Groups[3].Value switch { "单" => "odd", "双" => "even", _ => "all" };
                    courses.Add(ScheduleFileImport.MakeCourse(lesson.Name, weekday, settings.Periods[lesson.First - 1].Start,
                        settings.Periods[lesson.Last - 1].End, LocationValue().Match(content).Groups[1].Value,
                        TeacherValue().Match(content).Groups[1].Value, first, last, parity, "") with
                    { PeriodStart = lesson.First, PeriodEnd = lesson.Last });
                }
            }
        }
        return new ScheduleImportData(courses, courses.Count == 0 ? Periods(pages) : null, skipped);
    }

    private static List<SchedulePeriod>? Periods(IReadOnlyList<ScheduleTextPage> pages)
    {
        var periods = new SortedDictionary<int, SchedulePeriod>();
        if (!pages.Any(page => Rows(page.Boxes).Any(row => Tight(string.Concat(row.Select(box => box.Text))).Contains("节次", StringComparison.Ordinal))))
            return null;
        foreach (var row in pages.SelectMany(page => Rows(page.Boxes)))
        {
            var match = PeriodLine().Match(Tight(string.Concat(row.Select(box => box.Text))));
            if (match.Success && int.TryParse(match.Groups[1].Value, out var index))
                periods.TryAdd(index, new SchedulePeriod(NormalizeClock(match.Groups[2].Value), NormalizeClock(match.Groups[3].Value)));
        }
        return periods.Count >= 2 && periods.Keys.SequenceEqual(Enumerable.Range(1, periods.Count)) ? periods.Values.ToList() : null;
    }

    private static string NormalizeClock(string value) => value.Replace('：', ':').PadLeft(5, '0');

    private static List<Lesson> Lessons(IReadOnlyList<string> rows)
    {
        var matches = new List<Lesson>();
        for (var line = 0; line < rows.Count; line++)
        {
            var m = LessonHeading().Match(rows[line]);
            if (!m.Success)
                continue;
            var nameStart = line;
            for (var i = line - 1; i >= Math.Max(0, line - 3); i--)
            {
                if (MetadataLine().IsMatch(rows[i]) || LessonHeading().IsMatch(rows[i]))
                    break;
                nameStart = i;
            }
            var name = string.Concat(rows.Skip(nameStart).Take(line - nameStart)).Trim('★', '☆', '●', '○', '◇');
            if (name.Length > 0)
            {
                var first = int.Parse(m.Groups[1].Value);
                matches.Add(new Lesson(line, nameStart, name, first, m.Groups[2].Success ? int.Parse(m.Groups[2].Value) : first, m.Groups[3].Value));
            }
        }
        return matches;
    }

    private static IEnumerable<List<ScheduleTextBox>> Rows(IEnumerable<ScheduleTextBox> source)
    {
        List<ScheduleTextBox> row = [];
        foreach (var box in source.OrderBy(b => b.CenterY).ThenBy(b => b.X))
        {
            if (row.Count > 0 && box.CenterY - row[0].CenterY >= Math.Max(2, Math.Min(row[0].Height, box.Height) * .6))
            {
                yield return row.OrderBy(b => b.X).ToList();
                row = [];
            }
            row.Add(box);
        }
        if (row.Count > 0)
            yield return row.OrderBy(b => b.X).ToList();
    }

    private static IEnumerable<ScheduleTextBox> Segments(IReadOnlyList<ScheduleTextBox> row)
    {
        if (row.Count == 0)
            yield break;
        var first = row[0];
        var last = first;
        var text = new StringBuilder(first.Text);
        foreach (var box in row.Skip(1))
        {
            if (box.X - (last.X + last.Width) > Math.Max(first.Height, box.Height))
            {
                yield return Joined();
                first = box;
                text.Clear();
            }
            text.Append(box.Text);
            last = box;
        }
        yield return Joined();

        ScheduleTextBox Joined() => new(Tight(text.ToString()), first.X, first.Y, last.X + last.Width - first.X, first.Height);
    }

    private static string Tight(string value) => string.Concat(value.Where(c => !char.IsWhiteSpace(c)));
}
