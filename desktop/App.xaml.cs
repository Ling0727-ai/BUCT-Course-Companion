using System.Diagnostics;

namespace BuctCourseLocal;

public partial class App : Application
{
    private Mutex? instance;

    protected override void OnStartup(StartupEventArgs e)
    {
        base.OnStartup(e);
        instance = new Mutex(false, "Local\\BuctCourseLocal", out var first);
        if (!first)
        {
            MessageBox.Show("北化课程提醒已在运行，可以从 Windows 托盘打开。", "北化课程提醒");
            Shutdown();
            return;
        }

        try
        {
            var dataDir = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "BUCTCourseLocal");
            Directory.CreateDirectory(dataDir);
            MainWindow = new MainWindow(dataDir);
            MainWindow.Show();
        }
        catch (Exception ex)
        {
            Trace.TraceError("Initialization failed: {0}", ex.GetType().Name);
            MessageBox.Show("无法打开本地数据，请检查本应用是否可以写入 %LOCALAPPDATA%\\BUCTCourseLocal。", "北化课程提醒", MessageBoxButton.OK, MessageBoxImage.Error);
            Shutdown(1);
        }
    }

    protected override void OnExit(ExitEventArgs e)
    {
        instance?.Dispose();
        base.OnExit(e);
    }
}
