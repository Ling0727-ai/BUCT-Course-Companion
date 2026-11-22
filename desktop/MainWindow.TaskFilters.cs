using System.Windows.Controls.Primitives;

namespace BuctCourseLocal;

public partial class MainWindow
{
    private void Task_More(object sender, RoutedEventArgs e)
    {
        if (sender is not Button { DataContext: CourseTask task } button)
            return;
        var menu = new ContextMenu { PlacementTarget = button, Placement = PlacementMode.Bottom };
        void Add(string label, Action action)
        {
            var item = new MenuItem { Header = label };
            item.Click += (_, _) => action();
            menu.Items.Add(item);
        }
        if (task.HasNativeDetail)
            Add("查看详情与附件", () => _ = ViewDetailAsync(task.Kind, task.CourseId, task.Resource!.ItemId, task.Title,
                CourseService.TaskUri(task.Resource.Url)?.AbsoluteUri ?? task.Url, task.Details, fromCatalog: false));
        Add(task.IsTodo ? "移入回收站" : "隐藏这项任务", () => HideTask(task));
        if (!task.IsTodo && !string.IsNullOrWhiteSpace(task.Subject))
            Add("屏蔽这门课", () => ApplyTaskFilter(new TaskFilter(task.CourseKey, task.Subject, "course"), true));
        menu.IsOpen = true;
    }

    private void HideTask(CourseTask task) => ApplyTaskFilter(new TaskFilter(task.Id, task.Title, task.IsTodo ? "todo" : "task"), true);

    private void ApplyTaskFilter(TaskFilter filter, bool enabled)
    {
        store.SetFilter(filter, enabled);
        UpdateBoard();
        if (CatalogPanel.Visibility == Visibility.Visible)
            RenderCourseItems();
        SetStatus(enabled ? "已更新，随时可从隐藏与回收站恢复。" : "已恢复选中的记录。");
    }

    private void ManageFilters_Click(object sender, RoutedEventArgs e)
    {
        var dialog = new EditDialog(this, "隐藏与回收站");
        var list = new ListBox { DisplayMemberPath = "Label", MinHeight = 80, MaxHeight = 260 };
        void Reload()
        {
            var filters = store.Filters();
            list.ItemsSource = filters;
            dialog.ShowError("");
        }
        dialog.AddField("选择要恢复的记录", list);
        dialog.AddAction("关闭", dialog.Close, cancel: true);
        dialog.AddAction("恢复", () =>
        {
            if (list.SelectedItem is not TaskFilter filter)
            {
                dialog.ShowError("请先选择一条记录。");
                return;
            }
            ApplyTaskFilter(filter, false);
            Reload();
        }, primary: true);
        Reload();
        ShowDialog(dialog);
    }
}
