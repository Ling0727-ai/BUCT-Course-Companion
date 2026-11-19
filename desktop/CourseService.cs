using System.Diagnostics;
using System.Text;
using System.Text.Json;

namespace BuctCourseLocal;

internal record Credentials(string StudentId, string Password);
internal record CourseCookie(string Name, string Value, string Domain, string Path, long Expires, bool Secure, bool HttpOnly, string? SameSite);
internal record CourseRequest(string Mode, string? StudentId, string? Password, IReadOnlyList<CourseCookie> Cookies, string UserAgent,
    string CourseId, string ItemId);
internal record CourseResult(List<CourseTask>? Tasks, List<CourseCookie>? Cookies, bool NeedsLogin, string? Error,
    bool NewSession = false, List<string>? IncompleteKinds = null, string? Warning = null,
    List<CatalogCourse>? Courses = null, List<CatalogEntry>? Entries = null, ItemDetail? Detail = null);

internal static class CourseService
{
    internal static readonly JsonSerializerOptions Json = new(JsonSerializerDefaults.Web);
    internal const string Host = "course.buct.edu.cn";
    internal const string LoginUrl = "https://course.buct.edu.cn/meol/homepage/common/sso_login.jsp";
    internal const string HomeUrl = "https://course.buct.edu.cn/meol/index.do";
    internal const string LegacyHomePath = "/meol/homepage/student/index.jsp";
    internal const string StudentWelcomeUrl = "https://course.buct.edu.cn/meol/welcomepage/student/index.jsp";
    internal const string ReminderUrl = "https://course.buct.edu.cn/meol/welcomepage/student/interaction_reminder_v8.jsp";

    internal static bool IsCourseDomain(string domain) => domain.TrimStart('.').ToLowerInvariant() is Host or "buct.edu.cn";

    internal static Uri? TaskUri(string? raw) =>
        Uri.TryCreate(raw, UriKind.Absolute, out var uri) && uri.Scheme == Uri.UriSchemeHttps &&
        uri.IsDefaultPort && uri.Host == Host && uri.UserInfo.Length == 0 ? uri : null;

    internal static bool IsSchoolUri(Uri uri) => uri.Scheme == Uri.UriSchemeHttps && uri.IsDefaultPort &&
        uri.UserInfo.Length == 0 && (uri.Host == "buct.edu.cn" || uri.Host.EndsWith(".buct.edu.cn", StringComparison.Ordinal));

    internal static string ReadCourseId(string raw)
    {
        if (TaskUri(raw) is not { } url)
            return "";
        var query = System.Web.HttpUtility.ParseQueryString(url.Query);
        var id = query["courseId"] ?? query["cateId"] ?? "";
        return id.Length <= 32 && id.All(char.IsAsciiDigit) ? id : "";
    }

    internal static async Task<CourseResult> RunAsync(string mode, Credentials? account, IReadOnlyList<CourseCookie> cookies,
        string userAgent, CancellationToken appToken, string courseId = "", string itemId = "")
    {
        try
        {
            return await RunWorkerAsync<CourseRequest, CourseResult>(
                new CourseRequest(mode, account?.StudentId, account?.Password, cookies, userAgent, courseId, itemId),
                appToken, TimeSpan.FromMinutes(3));
        }
        catch (OperationCanceledException) when (!appToken.IsCancellationRequested)
        {
            return new CourseResult(null, null, false, "课程平台连接超时，请稍后重试。");
        }
    }

    internal static async Task<TReply> RunWorkerAsync<TRequest, TReply>(TRequest request, CancellationToken appToken,
        TimeSpan duration, string? argument = null) where TReply : class
    {
        using var timeout = CancellationTokenSource.CreateLinkedTokenSource(appToken);
        timeout.CancelAfter(duration);
        var token = timeout.Token;
        using var worker = new Process
        {
            StartInfo = new ProcessStartInfo(Path.Combine(AppContext.BaseDirectory, "course-worker.exe"))
            {
                UseShellExecute = false,
                CreateNoWindow = true,
                RedirectStandardInput = true,
                RedirectStandardOutput = true,
                RedirectStandardError = true,
                StandardInputEncoding = new UTF8Encoding(false),
                StandardOutputEncoding = Encoding.UTF8
            }
        };
        if (argument is not null)
            worker.StartInfo.ArgumentList.Add(argument);
        if (!worker.Start())
            throw new IOException("无法启动本地数据组件。");
        try
        {
            var stdout = worker.StandardOutput.ReadToEndAsync(token);
            var discard = worker.StandardError.BaseStream.CopyToAsync(Stream.Null, token);
            await worker.StandardInput.WriteAsync(JsonSerializer.Serialize(request, Json).AsMemory(), token);
            worker.StandardInput.Close();
            await worker.WaitForExitAsync(token);
            await discard;
            return JsonSerializer.Deserialize<TReply>(await stdout, Json)
                ?? throw new IOException("本地数据组件返回了空结果。");
        }
        finally
        {
            if (!worker.HasExited)
                worker.Kill(entireProcessTree: true);
        }
    }
}
