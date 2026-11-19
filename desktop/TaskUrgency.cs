namespace BuctCourseLocal;

public enum TaskUrgency
{
    Urgent,
    Upcoming,
    Normal,
    Completed
}

internal sealed record UrgencyBand(TaskUrgency Urgency, string Title, string Accent, string Tint)
{
    internal const long Day = 24 * 60 * 60;

    internal static readonly UrgencyBand[] All =
    [
        new(TaskUrgency.Urgent, "紧急", "#B42336", "#FFF1F3"),
        new(TaskUrgency.Upcoming, "即将到期", "#8B5204", "#FFF6E5"),
        new(TaskUrgency.Normal, "正常", "#126454", "#EAF6F1"),
        new(TaskUrgency.Completed, "已完成", "#526574", "#EEF3F6")
    ];

    internal static TaskUrgency Of(CourseTask task, long now)
    {
        if (task.Completed)
            return TaskUrgency.Completed;
        if (task.Deadline <= 0)
            return TaskUrgency.Normal;
        var remaining = task.Deadline - now;
        return remaining <= Day ? TaskUrgency.Urgent : remaining <= 3 * Day ? TaskUrgency.Upcoming : TaskUrgency.Normal;
    }

    internal static long NextChange(CourseTask task, long now)
    {
        if (task.Completed || task.Deadline <= now)
            return long.MaxValue;
        return Of(task, now) switch
        {
            TaskUrgency.Normal => task.Deadline - 3 * Day,
            TaskUrgency.Upcoming => task.Deadline - Day,
            _ => task.Deadline
        };
    }
}

internal sealed record TaskCard(CourseTask Task, bool Overdue)
{
    public string Timing => Overdue ? $"已逾期 · {Task.DueLabel}" : Task.DueLabel;
}

internal sealed record TaskGroup(UrgencyBand Band, IReadOnlyList<TaskCard> Cards)
{
    public int Count => Cards.Count;
}
