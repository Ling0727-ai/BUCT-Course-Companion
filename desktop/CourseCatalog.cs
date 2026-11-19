namespace BuctCourseLocal;

public record TaskResource(string CourseId, string ItemId, string Url);

internal record CatalogCourse(string Id, string Name, string Url, string Teacher, string Semester)
{
    public string Metadata => string.Join(" · ", new[] { Teacher, Semester }.Where(value => !string.IsNullOrWhiteSpace(value)));
}

internal record CatalogEntry(string Id, string Kind, string ItemId, string Title, string Status, string Start, string Deadline,
    string Score, string Url, string ResultUrl, bool Available)
{
    public string Timing => string.Join(" · ", new[]
    {
        Status, string.IsNullOrWhiteSpace(Start) ? "" : $"开始 {Start}", string.IsNullOrWhiteSpace(Deadline) ? "" : $"截止 {Deadline}",
        string.IsNullOrWhiteSpace(Score) ? "" : $"成绩 {Score}"
    }.Where(value => !string.IsNullOrWhiteSpace(value)));
    public bool HasResult => CourseService.TaskUri(ResultUrl) is not null;
    public bool HasDetail => ItemId.Length > 0;
    public string OpenLabel => Kind == "test" ? "打开测试列表" : "打开作业页面";
}

internal record CourseAttachment(string Name, string Url);
internal record ItemDetail(string Title, string Text, string Start, string Deadline, string Score, string Publisher,
    string Duration, string Questions, List<CourseAttachment>? Attachments);

internal record TaskFilter(string Key, string Title, string Kind)
{
    public string Label => Kind switch { "course" => $"屏蔽课程 · {Title}", "todo" => $"待办回收站 · {Title}", _ => $"隐藏任务 · {Title}" };
}
