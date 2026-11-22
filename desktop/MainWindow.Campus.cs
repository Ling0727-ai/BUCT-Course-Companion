using System.Diagnostics;
using Microsoft.Web.WebView2.Core;

namespace BuctCourseLocal;

public partial class MainWindow
{
    private sealed record CampusPage(string Title, Uri Entry, bool Mail = false);

    private CampusPage? campusPage;
    private bool campusSawIdentity;
    private bool campusTriedCredentials;

    private async void CampusLink_Click(object sender, RoutedEventArgs e)
    {
        if (sender is not Button { Tag: string key })
            return;
        var page = key switch
        {
            "portal" => new CampusPage("i北化", new Uri("https://i.buct.edu.cn/")),
            "academic" => new CampusPage("本科教务", IdentityService("https://jwglxt.buct.edu.cn/sso/jziotlogin")),
            "mail" => new CampusPage("校园邮箱", IdentityService("https://mailuser.buct.edu.cn/buct/api/login"), Mail: true),
            "library" => new CampusPage("图书馆", new Uri("https://library.buct.edu.cn/")),
            "card" => new CampusPage("校园卡服务", new Uri("https://cit.buct.edu.cn/ykt_7863/list.htm")),
            "vpn" => new CampusPage("校园 VPN", new Uri("https://w.buct.edu.cn/")),
            _ => null
        };
        if (page is not null)
            await OpenCampusAsync(page);
    }

    private static Uri IdentityService(string service) => new("https://sidportal.buct.edu.cn/login?service=" + Uri.EscapeDataString(service));

    private bool IsBrowserUri(Uri uri) => CourseService.IsSchoolUri(uri) ||
        (campusPage?.Mail == true && CourseService.IsHttps(uri) && uri.Host == "mailh.qiye.163.com");

    private async Task OpenCampusAsync(CampusPage page)
    {
        var request = ++openRequest;
        requestedCoursePage = null;
        activeNavigation = 0;
        queuedRecovery = null;
        afterLogin = null;
        browseAfterLogin = null;
        queuedBrowse = null;
        syncAfterLogin = false;
        browserLoginReady = false;
        LoginBanner.Visibility = Visibility.Collapsed;
        campusPage = page;
        campusSawIdentity = false;
        campusTriedCredentials = false;
        BrowserTitle.Text = page.Title;
        BrowserHost.Text = "";
        CampusBackButton.Visibility = Visibility.Visible;
        ShowPage(BrowserPanel, DashboardButton);
        SetBrowserFeedback($"正在打开{page.Title}…", loading: true);
        try
        {
            await EnsureBrowserAsync();
            if (request == openRequest && !exiting)
                web!.Navigate(page.Entry.AbsoluteUri);
        }
        catch (OperationCanceledException) when (exiting) { }
        catch (Exception ex)
        {
            Trace.TraceError("Opening campus page failed: {0}", ex.GetType().Name);
            if (request == openRequest && !exiting)
                SetBrowserFeedback(ex is WebView2RuntimeNotFoundException ? "找不到 Microsoft Edge WebView2 Runtime。" :
                    $"暂时无法打开{page.Title}，请重试。", retry: true);
        }
    }

    private async Task CompleteCampusNavigationAsync(CoreWebView2NavigationCompletedEventArgs args, CampusPage page)
    {
        if (args.WebErrorStatus == CoreWebView2WebErrorStatus.OperationCanceled)
            return;
        if (!args.IsSuccess || args.HttpStatusCode >= 400)
        {
            SetBrowserFeedback($"{page.Title}加载失败，请检查网络后重试。", retry: true);
            return;
        }
        var request = openRequest;
        if (Uri.TryCreate(web!.Source, UriKind.Absolute, out var uri) && CourseService.IsIdentityLogin(uri))
        {
            SetBrowserFeedback("请在此页面完成学校认证，认证后会继续打开所选服务。", retry: campusTriedCredentials);
            if (!campusTriedCredentials && account is { } credentials && BrowserPanel.IsVisible)
            {
                if (busy)
                {
                    SetBrowserFeedback("正在处理课程同步。可以在此登录，或稍后重试。", retry: true);
                    return;
                }
                campusTriedCredentials = true;
                await RunAsync("", async () =>
                {
                    SetBrowserFeedback("正在尝试复用已保存的学校凭证。需要二次验证时，可直接在此完成。", loading: true);
                    var result = await RunCourseAsync("login", credentials, []);
                    if (exiting || request != openRequest || args.NavigationId != activeNavigation)
                        return;
                    if (!result.NeedsLogin && result.Error is null && result.IdentityCookies?.Count > 0)
                    {
                        await ApplyCourseSessionAsync(result);
                        if (!exiting && request == openRequest && args.NavigationId == activeNavigation)
                        {
                            SetBrowserFeedback("正在继续打开校园服务…", loading: true);
                            web!.Navigate(page.Entry.AbsoluteUri);
                        }
                        return;
                    }
                    SetBrowserFeedback("自动认证未完成。请在此页登录，或重试自动认证。", retry: true);
                }, browserOperation: true);
            }
            return;
        }

        SetBrowserFeedback("");
        if (campusSawIdentity)
        {
            campusSawIdentity = false;
            try { await PersistBrowserCookiesAsync(request); }
            catch (Exception ex) { Trace.TraceWarning("Campus session persistence failed: {0}", ex.GetType().Name); }
        }
    }

    private void ResetCampusPage()
    {
        if (campusPage is null)
            return;
        campusPage = null;
        campusSawIdentity = false;
        activeNavigation = 0;
        BrowserTitle.Text = "课程平台";
        BrowserHost.Text = "";
        CampusBackButton.Visibility = Visibility.Collapsed;
        SetBrowserFeedback("");
    }

    private async void CampusRetry_Click(object sender, RoutedEventArgs e)
    {
        if (campusPage is { } page)
            await OpenCampusAsync(page);
    }
}
