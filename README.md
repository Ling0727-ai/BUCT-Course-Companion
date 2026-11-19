# 北化课伴 · BUCT Course Companion

面向北京化工大学学生的 Windows 桌面应用。在一个窗口里查看北化在线的作业和测验、安排个人待办、管理课表，并接收截止与课前提醒。

## 开始使用

需要 **Windows 10 1809 或 Windows 11（x64）**。下载 `BUCT-Course-Companion-*-Setup-x64.exe`，双击安装后从开始菜单打开「北化课伴」。安装程序会在需要时补装 WebView2 和 Visual C++ 运行库。如果缺少 WebView2，安装时需要联网。升级前先从托盘退出应用。[GitHub Releases](https://github.com/Ling0727-ai/BUCT-Course-Companion/releases) · [Windows 安装包构建记录](https://github.com/Ling0727-ai/BUCT-Course-Companion/actions/workflows/windows.yml)

1. 打开 **账户与通知**。可以选择 **在内置页面登录**，也可以保存学号和密码，供会话过期时自动登录。遇到验证码或二次验证时，在学校登录页完成认证。
2. 在 **课程与待办** 点击 **同步课程**。同步成功后，应用每 3 小时自动刷新，也可以随时手动刷新。应用运行期间每 25 分钟校验一次登录会话，读取学校轮换的 Cookie。如果已知 Cookie 更早过期，则提前校验。会话过期时，应用会用已保存的凭证尝试恢复。
3. 点击任务卡上的 **打开课程页面**。应用优先复用课程会话，从内置浏览器打开所属课程或测验列表。需要重新登录时，认证完成后会继续打开原来的页面。

个人待办、已缓存的课程信息和本地课表可离线查看。

## Linux、macOS 和 Android

三个平台使用共用的 Go 桌面和移动界面，提供作业与测试同步、紧急度筛选、回收站、课程详情、个人待办、SQLite 周课表与考试管理。第一次连接学校账号时，可选择用单独的保护密码加密保存凭证。重启后需要解锁。

| 平台 | 安装包 |
| --- | --- |
| Linux（Debian / Ubuntu，amd64） | `.deb` |
| macOS（Apple Silicon 或 Intel） | 对应架构的 `.pkg` |
| Android（arm64） | `.apk` |

学校页面和附件由系统浏览器打开。应用内同步使用学校账号连接，浏览器完成的验证码或二次验证目前不能回传给同步组件。便携版课表可导入课序 JSON 或 CSV，导出 JSON 和 ICS。Windows 原生版提供 Excel、PDF 和图片导入。Android 会向系统登记未来 36 小时内最早的 60 条提醒，登录保活在应用运行时检查。

安装包由 [多平台构建工作流](https://github.com/Ling0727-ai/BUCT-Course-Companion/actions/workflows/portable.yml) 生成。Android 默认使用独立包名的预览签名。如需正式签名，在 GitHub Actions 设置 `BUCT_ANDROID_KEYSTORE_B64`、`BUCT_ANDROID_STORE_PASSWORD`、`BUCT_ANDROID_KEY_PASSWORD` 和 `BUCT_ANDROID_KEY_ALIAS`。macOS 包使用临时签名以便启动二进制，正式分发需另配 Developer ID 和公证。

## 任务与提醒

- **查任务**。待办按紧急、即将到期、正常和已完成分组。紧急包含 24 小时内截止和未完成的逾期任务。即将到期指剩余超过 24 小时且不超过 3 天。点击统计卡片筛选，可与作业、测验和个人待办类型叠加。
- **查课程**。在「我的课程」搜索课程，查看该门课的作业、测验和学校提供的结果入口。任务卡的「···」可打开可用的作业要求、附件和测试说明。
- **清理任务**。在任务卡菜单隐藏单项任务、屏蔽课程，或将个人待办移入回收站。在「隐藏与回收站」恢复记录。
- **收提醒**。任务默认在截止前 24、12、1 小时提醒。同一任务、截止时间和时段只提醒一次。在「账户与通知」可调整开关或发送测试通知。

关闭窗口后，应用会留在 Windows 托盘继续同步和提醒。通过托盘菜单选择「退出」可结束后台运行。系统通知的显示由 Windows 通知设置控制。

## 管理课表

在 **课表** 中按周查看课程和考试，点击卡片编辑。课程支持按钟点或节次安排、单双周和单次停课。「学期与作息」可设置节次时间、停课与补课，并开启课程和考试的提前提醒。

| 操作 | 支持的文件 |
| --- | --- |
| 导入 | CSV、Excel、ICS、PDF、PNG、JPG |
| 导出 | ICS、课程 CSV、作息 CSV |

导入时先设置学期的起始日期和周数，再在预览里核对时间、地点和教学周。PDF 与图片在本机识别，识别到的项目会先显示在预览中。

## 数据与迁移

- **本地数据**保存在 `%LOCALAPPDATA%\BUCTCourseLocal\course.db`，包括任务、课表、课程缓存和提醒记录。同目录的 `WebView2` 保存内置浏览器的配置。应用保存的学校凭证与课程会话由 Windows DPAPI 加密，绑定当前 Windows 用户。
- **课序数据**。首次创建本地课表时，应用会尝试导入 `%APPDATA%\CourseFlow\schedule.json`。如果没有这个文件，则读取桌面旧版已导入的课表。以后在北化课伴中编辑的课表保存在 SQLite。
- **网页旧版任务**。将同一用户的 `course_data`、`todos` 或 `assignment_status` 从旧版 MongoDB 导出为同名 `.json` 文件，支持逐行 JSON 和 JSON 数组。在「账户与通知 → 导入旧版 MongoDB 导出文件」中选择文件。需要带上作业的完成状态时，请同时选择 `course_data.json` 和 `assignment_status.json`。

## 从源码构建

需要 **.NET 8 SDK 或更新版本**及 **Go 1.25.5 或更新版本**。在项目根目录运行以下命令。桌面项目会自动构建并复制 Go 组件。

```powershell
dotnet run --project .\desktop\BuctCourseLocal.csproj
```

生成包含 .NET 运行时的 Windows x64 发布目录。

```powershell
dotnet publish .\desktop\BuctCourseLocal.csproj -c Release --self-contained true -o .\dist\BUCT-Course-Companion
```

生成 `.exe` 安装包。需要 PowerShell 7 和 Inno Setup 6，产物位于 `dist\windows-installer`。

```powershell
pwsh -File .\packaging\windows\Build-Installer.ps1 -Version 0.1.0
```

推送到 `main` 后，GitHub Actions 会运行 Go 测试、打包并验证安装和卸载。每次运行的 **Artifacts → windows-x64-installer** 提供安装包。推送形如 `v0.1.0` 的版本标签会将同一个 `.exe` 发布到对应的 GitHub Release。

运行 Go 组件测试。

```powershell
Push-Location .\course-worker
go test ./...
Pop-Location
```

便携客户端的源码运行需要 C 编译器和 Fyne 的平台依赖。安装包脚本分别使用目标系统的 `dpkg-deb`、`pkgbuild`，或 Android SDK、NDK、Fyne CLI 和 `bundletool`。平台依赖见 [Fyne 环境说明](https://docs.fyne.io/started/quick/)。

```text
bash packaging/portable/package-desktop.sh linux 0.1.0
bash packaging/portable/package-desktop.sh macos 0.1.0
bash packaging/portable/package-android.sh 0.1.0 1
```

| 目录 | 主要职责 |
| --- | --- |
| [`desktop/`](desktop/) | WPF 界面、WebView2、SQLite、文件导入导出和 Windows 托盘 |
| [`course-worker/`](course-worker/) | 学校会话、作业与测验同步、课表校验和提醒计算 |
| [`packaging/windows/`](packaging/windows/) | Windows 安装包构建和 Inno Setup 配置 |
| [`course-worker/cmd/buct-portable/`](course-worker/cmd/buct-portable/) | Linux、macOS 和 Android 共用的原生界面 |

## 来源与许可

- 作业与测验同步复用 `BUCT-course-remind` 的 scraper 和 `go-buct-course` 客户端。[BUCT-course-remind 源码](https://github.com/Ling0727-ai/BUCT-course-remind/tree/6563bba793df487c8ba46bc87e6cae83a3bfabe5/web/go-backend-test/scraper) · [go-buct-course](https://github.com/Ling0727-ai/go-buct-course)
- 课表数据模型、校历规则、课表版式识别和图标来自 `course-schedule`（课序 / CourseFlow）。浅色磨砂与胶囊导航参考了 BUCT One 的视觉设计。[BUCT One](https://github.com/STAR-10086/buct-one)

项目许可见 [MIT License](LICENSE)。
