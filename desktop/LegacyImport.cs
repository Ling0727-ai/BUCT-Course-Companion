using System.Globalization;
using System.Text.Json;

namespace BuctCourseLocal;

internal static class LegacyImport
{
    internal static List<CourseTask> Read(IEnumerable<string> files)
    {
        var rows = new List<(string Collection, JsonElement Row)>();
        foreach (var file in files)
        {
            var collection = Path.GetFileNameWithoutExtension(file).ToLowerInvariant();
            if (collection is not ("course_data" or "todos" or "assignment_status"))
                throw new FormatException();
            var data = File.ReadAllText(file).Trim();
            if (data.StartsWith('['))
            {
                using var document = JsonDocument.Parse(data);
                rows.AddRange(document.RootElement.EnumerateArray().Select(row => (collection, row.Clone())));
            }
            else
            {
                foreach (var line in data.Split('\n', StringSplitOptions.RemoveEmptyEntries | StringSplitOptions.TrimEntries))
                {
                    using var document = JsonDocument.Parse(line);
                    rows.Add((collection, document.RootElement.Clone()));
                }
            }
        }
        if (rows.Any(r => r.Row.ValueKind != JsonValueKind.Object || Field(r.Row, "user_id").Length == 0) ||
            rows.Select(r => Field(r.Row, "user_id")).Distinct().Take(2).Count() > 1)
            throw new FormatException();

        var states = rows.Where(r => r.Collection == "assignment_status").GroupBy(r => Field(r.Row, "assignment_id"))
            .ToDictionary(g => g.Key, g => Field(g.Last().Row, "status"));
        var tasks = new List<CourseTask>();
        foreach (var (collection, row) in rows)
        {
            if (collection == "assignment_status")
                continue;
            var todo = collection == "todos";
            var kind = todo ? "todo" : Field(row, "type");
            var id = todo ? Field(row, "_id") : Field(row, "task_id");
            var title = Field(row, "title");
            var subject = todo ? "个人" : Field(row, "subject");
            var prefix = subject + " - ";
            if (kind == "test" && title.StartsWith(prefix, StringComparison.Ordinal))
                title = title[prefix.Length..];
            if (id.Length == 0 || title.Length == 0 || kind is not ("todo" or "test" or "homework"))
                throw new FormatException();
            states.TryGetValue(id, out var state);
            if (todo && (Flag(row, "is_deleted") || Field(row, "forever") == "0") || !todo && state == "deleted")
                continue;
            var details = Field(row, todo ? "description" : "details");
            var url = todo ? "" : CourseService.TaskUri(Field(row, "url"))?.AbsoluteUri ?? "";
            var deadline = ReadDeadline(Field(row, todo ? "due_date" : "deadline"));
            tasks.Add(new CourseTask($"legacy:{kind}:{id}", kind, subject, title, details, url, deadline,
                todo ? Flag(row, "completed") : state == "completed"));
        }
        return tasks;
    }

    private static string Field(JsonElement row, string name)
    {
        if (!row.TryGetProperty(name, out var value))
            return "";
        if (value.ValueKind == JsonValueKind.Object)
            return value.TryGetProperty("$date", out var date) ? date.ToString() :
                value.TryGetProperty("$oid", out var oid) ? oid.ToString() : "";
        return value.ValueKind == JsonValueKind.Null ? "" : value.ToString();
    }

    private static bool Flag(JsonElement row, string name) => row.TryGetProperty(name, out var value) && value.ValueKind == JsonValueKind.True;

    private static long ReadDeadline(string input)
    {
        if (DateTime.TryParseExact(input, ["yyyy-MM-dd HH:mm:ss", "yyyy年M月d日 HH:mm:ss"], CultureInfo.InvariantCulture, DateTimeStyles.None, out var local))
            return Math.Max(0, new DateTimeOffset(local, CourseTask.BeijingOffset).ToUnixTimeSeconds());
        return DateTimeOffset.TryParse(input, CultureInfo.InvariantCulture, DateTimeStyles.AssumeUniversal, out var instant)
            ? Math.Max(0, instant.ToUnixTimeSeconds()) : 0;
    }
}
