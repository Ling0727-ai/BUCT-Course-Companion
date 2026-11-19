using System.Windows.Media;

namespace BuctCourseLocal;

internal sealed class EditDialog : Window
{
    private readonly StackPanel fields = new();
    private readonly StackPanel actions = new() { Orientation = Orientation.Horizontal, HorizontalAlignment = HorizontalAlignment.Right, Margin = new Thickness(0, 18, 0, 0) };
    private readonly TextBlock error = new() { TextWrapping = TextWrapping.Wrap, Foreground = Brushes.Firebrick, Margin = new Thickness(0, 8, 0, 0) };

    internal EditDialog(Window owner, string title)
    {
        Owner = owner;
        Title = title;
        Width = 470;
        SizeToContent = SizeToContent.Height;
        ResizeMode = ResizeMode.NoResize;
        ShowInTaskbar = false;
        WindowStartupLocation = WindowStartupLocation.CenterOwner;
        Background = (Brush)FindResource("GlassBackdrop");
        Foreground = (Brush)FindResource("Ink");
        FontFamily = new FontFamily("Segoe UI Variable Text, Segoe UI, Microsoft YaHei UI");
        var panel = new StackPanel();
        panel.Children.Add(new TextBlock { Text = title, FontSize = 23, FontWeight = FontWeights.SemiBold, Margin = new Thickness(0, 0, 0, 6) });
        panel.Children.Add(new ScrollViewer { Content = fields, MaxHeight = 430, VerticalScrollBarVisibility = ScrollBarVisibility.Auto,
            HorizontalScrollBarVisibility = ScrollBarVisibility.Disabled });
        panel.Children.Add(error);
        panel.Children.Add(actions);
        Content = new Border
        {
            Style = (Style)FindResource("FrostedPanel"), Margin = new Thickness(10), Padding = new Thickness(16), Child = panel
        };
    }

    internal void AddField(string label, Control control)
    {
        fields.Children.Add(new Label { Content = label, Target = control, Padding = new Thickness(0, 9, 0, 5) });
        fields.Children.Add(control);
    }

    internal void AddDescription(string text)
    {
        fields.Children.Add(new TextBlock
        {
            Text = text, TextWrapping = TextWrapping.Wrap, Margin = new Thickness(0, 7, 0, 4),
            Foreground = (Brush)FindResource("Muted")
        });
    }

    internal void AddOption(Button button)
    {
        button.Margin = new Thickness(0, 8, 0, 0);
        fields.Children.Add(button);
    }

    internal void AddAction(string label, Action handler, bool primary = false, bool cancel = false)
    {
        var button = new Button { Content = label, Margin = new Thickness(7, 0, 0, 0), IsDefault = primary, IsCancel = cancel };
        if (primary)
            button.Style = (Style)FindResource("PrimaryButton");
        button.Click += (_, _) => handler();
        actions.Children.Add(button);
    }

    internal void ShowError(string message) => error.Text = message;
}
