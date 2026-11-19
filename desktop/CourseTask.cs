using System.Globalization;

namespace BuctCourseLocal;

public record CourseTask(string Id, string Kind, string Subject, string Title, string Details, string Url, long Deadline,
    bool Completed = false, TaskResource? Resource = null)
{
    public static readonly TimeSpan BeijingOffset = TimeSpan.FromHours(8);

    public bool IsTodo => Kind == "todo";
    public bool HasLink => CourseService.TaskUri(Url) != null;
    public bool HasNativeDetail => !IsTodo && !string.IsNullOrWhiteSpace(Resource?.ItemId);
    internal string CourseId => Resource?.CourseId is { Length: > 0 } id ? id : CourseService.ReadCourseId(Url);
    internal string CourseKey => CourseId is { Length: > 0 } id ? $"id:{id}" : $"name:{Subject}";
    public string KindLabel => Kind switch { "homework" => "作业", "test" => "测试", _ => "待办" };
    public string OpenLabel => IsTodo ? "编辑待办" : "打开课程页面 ↗";
    public string StatusAction => Completed ? "恢复待办" : "标记完成";
    public Visibility DetailsVisibility => string.IsNullOrWhiteSpace(Details) ? Visibility.Collapsed : Visibility.Visible;
    public string DueLabel => Deadline <= 0 ? "无截止时间" : $"截止 {BeijingTime(Deadline):MM月dd日 HH:mm}（北京时间）";

    internal static DateTimeOffset BeijingTime(long seconds) => DateTimeOffset.FromUnixTimeSeconds(seconds).ToOffset(BeijingOffset);

    internal static bool TryParseDue(string input, out long deadline)
    {
        deadline = 0;
        if (string.IsNullOrWhiteSpace(input))
            return true;
        if (!DateTime.TryParseExact(input.Trim(), "yyyy-MM-dd HH:mm", CultureInfo.InvariantCulture, DateTimeStyles.None, out var time))
            return false;
        deadline = new DateTimeOffset(time, BeijingOffset).ToUnixTimeSeconds();
        return deadline > 0;
    }
}
