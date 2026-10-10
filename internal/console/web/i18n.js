"use strict";
(() => {
 const pairs = [
 ["Type the complete target name or access key again; it must exactly match the field above", "再次输入完整的目标名称或 Access Key，必须与上方对应字段完全一致"],
 ["At least 8 characters; leave blank to generate when optional", "至少 8 个字符；可选时留空自动生成"],
 ["Secret key must contain 8–128 characters. Leave it blank to generate one when optional.", "Secret Key 长度必须为 8–128 个字符；可选时留空自动生成。"],

 ["The policy Resource refers to a different bucket. Use the target bucket name in every S3 resource ARN.", "策略 Resource 指向其他存储桶。请将每个 S3 资源 ARN 中的桶名改为当前目标桶名。"],
 ["Enter a valid JSON policy document.", "请输入有效的 JSON 策略文档。"],

 ["The storage request is invalid.", "存储请求无效，请刷新对象列表后重试。"],
["Rename object", "重命名对象"],
["New file name", "新文件名"],
["Confirm", "确定"],
["Rename copies the object, then deletes the original. Existing objects are never overwritten. Avoid modifying the original concurrently.", "重命名先复制再删除原对象，不覆盖已有文件。请避免同时修改原对象。"],
["Enter a file name without path separators.", "请输入不含路径分隔符的文件名。"],
["Enter a different file name.", "请输入不同的文件名。"],
["Renaming…", "正在重命名…"],
["Connecting…", "正在连接…"],
["Link created. Copy it before closing this window.", "链接已创建，请在关闭窗口前复制。"],
["Writes are disabled.", "写入已禁用。"],
["This connection does not support renaming.", "此连接不支持重命名。"],
["Choose a different valid object name.", "请选择另一个有效的对象名称。"],
["Rename supports objects up to 5 GiB.", "重命名支持最大 5 GiB 的对象。"],
["The new object was copied, but the source changed or could not be checked. Both names may exist; refresh before retrying.", "新对象已复制，但原对象发生变化或无法检查。两个名称可能同时存在，请刷新后再操作。"],
["The new object was copied, but source deletion was not confirmed. Check both names before retrying.", "新对象已复制，但未确认原对象删除成功。请检查两个名称后再操作。"],
  [
    "Local console",
    "本地控制台"
  ],
  [
    "OC · Local console",
    "OC · 本地控制台"
  ],
  [
    "OC Local console",
    "OC 本地控制台"
  ],
  [
    "● Read-only",
    "● 只读"
  ],
  [
    "● Writes enabled",
    "● 已启用写入"
  ],
  [
    "● Restart required",
    "● 需要重启"
  ],
  [
    "Access management",
    "访问管理"
  ],
  [
    "Account",
    "账户"
  ],
  [
    "Refresh",
    "刷新"
  ],
  [
    "Log out",
    "退出登录"
  ],
  [
    "YOUR STORAGE, LOCALLY",
    "在本机管理你的存储"
  ],
  [
    "A clear view of",
    "清晰查看"
  ],
  [
    " your objects.",
    "你的对象。"
  ],
  [
    "your objects.",
    "你的对象。"
  ],
  [
    "Browse buckets, inspect objects, and download files from the alias selected in your OC terminal.",
    "浏览存储桶、查看对象并下载文件，使用 OC 启动时选择的存储身份。"
  ],
  [
    "Connect to OC",
    "连接 OC"
  ],
  [
    "Login code",
    "登录码"
  ],
  [
    "Use the login code printed by",
    "使用以下程序输出的登录码："
  ],
  [
    ". Your storage credentials stay in the local OC process.",
    "。存储凭据保留在本机 OC 进程中。"
  ],
  [
    "Open console",
    "打开控制台"
  ],
  [
    "Writes are disabled by default. The OC process must explicitly enable them.",
    "默认关闭写入，需由 OC 进程显式开启。"
  ],
  [
    "Object storage",
    "对象存储"
  ],
  [
    "Overview",
    "概览"
  ],
  [
    "Buckets",
    "存储桶列表"
  ],
  [
    "Favorites",
    "收藏路径"
  ],
  [
    "Operations",
    "运维管理"
  ],
  [
    "Tasks",
    "任务中心"
  ],
  [
    "Account & permissions",
    "账户与权限"
  ],
  [
    "One connection · local session",
    "单连接 · 本机会话"
  ],
  [
    "Storage validates operation permissions",
    "操作权限由存储服务验证"
  ],
  [
    "Usage overview",
    "用量概览"
  ],
  [
    "Capacity summary of buckets accessible to this identity; provided by the server, not billing data.",
    "当前身份可访问桶的容量摘要；由服务端提供，不作为计费数据。"
  ],
  [
    "Visible bucket capacity",
    "可见桶容量"
  ],
  [
    "Listed buckets",
    "列出的存储桶"
  ],
  [
    "Current session tasks",
    "当前会话任务"
  ],
  [
    "Recently visited buckets",
    "最近访问桶"
  ],
  [
    "Recent visits are saved for the current storage identity.",
    "按当前存储身份保存最近访问记录。"
  ],
  [
    "Common actions",
    "常用操作"
  ],
  [
    "Browse objects and prefixes, download and share. Writes must be explicitly enabled by the local process.",
    "查看桶内对象、浏览目录前缀、下载与分享。写操作需要本机进程显式开启。"
  ],
  [
    "Browse buckets →",
    "浏览存储桶 →"
  ],
  [
    "Connection status",
    "连接状态"
  ],
  [
    "Monthly traffic, request trends and alerts are unavailable. View task results in Tasks.",
    "月度流量、请求趋势及告警尚未接入。任务结果可在任务中心查看。"
  ],
  [
    "Connected to",
    "当前连接"
  ],
  [
    "Local session",
    "本机会话"
  ],
  [
    "Favorites are saved for the current storage identity and restored on login.",
    "收藏按当前存储身份保存，重新登录后恢复。"
  ],
  [
    "Create bucket",
    "创建存储桶"
  ],
  [
    "Permission information",
    "授权信息"
  ],
  [
    "Search bucket names",
    "搜索存储桶名称"
  ],
  [
    "Bucket name",
    "存储桶名称"
  ],
  [
    "Permission summary",
    "权限摘要"
  ],
  [
    "Storage capacity",
    "存储容量"
  ],
  [
    "Created",
    "创建时间"
  ],
  [
    "Actions",
    "操作"
  ],
  [
    "← Back to buckets",
    "← 返回桶列表"
  ],
  [
    "Files",
    "文件列表"
  ],
  [
    "Settings",
    "配置管理"
  ],
  [
    "Refresh files",
    "刷新文件"
  ],
  [
    "Bucket",
    "存储桶"
  ],
  [
    "ONE ALIAS · ONE SESSION",
    "一个别名 · 一个会话"
  ],
  [
    "Only the alias selected at startup is available here.",
    "这里只使用启动时选择的别名。"
  ],
  [
    "OBJECT BROWSER",
    "对象浏览器"
  ],
  [
    "Choose a bucket",
    "选择存储桶"
  ],
  [
    "Delete empty bucket",
    "删除空桶"
  ],
  [
    "Version history",
    "版本历史"
  ],
  [
    "Bucket settings",
    "桶配置"
  ],
  [
    "Parent folder",
    "上级目录"
  ],
  [
    "Prefix",
    "目录前缀"
  ],
  [
    "Search",
    "查找"
  ],
  [
    "Root",
    "根目录"
  ],
  [
    "Upload file",
    "上传文件"
  ],
  [
    "Delete by key",
    "按路径删除"
  ],
  [
    "Delete selected",
    "删除选中对象"
  ],
  [
    "Delete current prefix",
    "删除当前前缀"
  ],
  [
    "No objects selected",
    "未选择对象"
  ],
  [
    "Writes enabled for this local session. Each object operation is checked by the storage server.",
    "本机会话已启用写入，每次操作均由存储服务检查权限。"
  ],
  [
    "Download selected as ZIP",
    "下载选中对象为 ZIP"
  ],
  [
    "Download this prefix as ZIP",
    "下载当前前缀为 ZIP"
  ],
  [
    "ZIP task",
    "ZIP 任务"
  ],
  [
    "Select a bucket",
    "选择存储桶"
  ],
  [
    "Browse its prefixes and download objects.",
    "浏览目录前缀并下载对象。"
  ],
  [
    "Try again",
    "重试"
  ],
  [
    "Objects in the selected bucket and prefix",
    "当前桶与前缀中的对象"
  ],
  [
    "Filename",
    "文件名"
  ],
  [
    "Size",
    "大小"
  ],
  [
    "Modified",
    "修改时间"
  ],
  [
    "Action",
    "操作"
  ],
  [
    "Load more",
    "加载更多"
  ],
  [
    "Server permissions",
    "服务端权限"
  ],
  [
    "Select a bucket to view available permission information.",
    "选择存储桶以查看权限信息。"
  ],
  [
    "No upload or deletion tasks in this session. Open ZIP task in the object browser to check ZIP preparation.",
    "当前会话暂无上传或删除任务。ZIP 准备状态可从对象浏览器的 ZIP task 查看。"
  ],
  [
    "THIS SESSION",
    "当前会话"
  ],
  [
    "Transfers & deletions",
    "传输与删除"
  ],
  [
    "Clear finished",
    "清除已完成任务"
  ],
  [
    "Tasks keep their original bucket and object scope when you navigate. Cancellation does not undo completed work.",
    "切换页面不改变任务的目标桶和对象范围。取消不会撤销已完成的操作。"
  ],
  [
    "Read-only browsing. Downloads use your browser’s download manager. If a download fails, its error opens in a separate tab.",
    "只读浏览。下载由浏览器管理；失败时错误会在新标签页显示。"
  ],
  [
    "Image preview",
    "图片预览"
  ],
  [
    "Close preview",
    "关闭预览"
  ],
  [
    "Zoom out",
    "缩小"
  ],
  [
    "Zoom in",
    "放大"
  ],
  [
    "Fit to window",
    "适应窗口"
  ],
  [
    "Object details",
    "文件详情"
  ],
  [
    "Close",
    "关闭"
  ],
  [
    "Full object key",
    "完整对象路径"
  ],
  [
    "Local session download link",
    "本机会话下载链接"
  ],
  [
    "This link requires the current local session. Use Share link for external sharing.",
    "该链接需要当前本机登录会话。对外分享请使用“分享链接”。"
  ],
  [
    "Read object information",
    "读取对象信息"
  ],
  [
    "Preview image",
    "预览图片"
  ],
  [
    "Share link",
    "分享链接"
  ],
  [
    "Copy local link",
    "复制本机链接"
  ],
  [
    "Read and edit one complete setting at a time. Each request uses your configured identity. Bucket browsing permission is not required to enter a known bucket.",
    "每次读取或编辑一项完整配置，使用当前存储身份。可直接输入已知桶名，无需浏览桶权限。"
  ],
  [
    "Target bucket",
    "目标存储桶"
  ],
  [
    "Setting",
    "配置项"
  ],
  [
    "Bucket policy",
    "桶策略"
  ],
  [
    "Versioning",
    "版本控制"
  ],
  [
    "Lifecycle",
    "生命周期"
  ],
  [
    "Read setting",
    "读取配置"
  ],
  [
    "Complete configuration",
    "完整配置"
  ],
  [
    "The complete document is submitted as entered. Storage validates supported fields and may normalize the saved configuration. Reading over unsaved edits requires confirmation; a failed read keeps the draft. OC never retries a conflicting write automatically.",
    "完整文档按输入内容提交，存储服务验证字段并可能规范化配置。覆盖未保存编辑前需确认；读取失败会保留草稿。OC 不自动重试冲突写入。"
  ],
  [
    "Review removal",
    "确认移除"
  ],
  [
    "Review change",
    "确认修改"
  ],
  [
    "Replace unsaved draft?",
    "替换未保存的草稿？"
  ],
  [
    "Keep draft",
    "保留草稿"
  ],
  [
    "A successful read replaces the unsaved configuration in the editor. No storage setting is changed by reading.",
    "读取成功后替换编辑器中的草稿，读取不会修改存储配置。"
  ],
  [
    "Current draft",
    "当前草稿"
  ],
  [
    "Read next",
    "即将读取"
  ],
  [
    "Read and replace draft",
    "读取并替换草稿"
  ],
  [
    "Review bucket setting",
    "确认桶配置"
  ],
  [
    "Back",
    "返回"
  ],
  [
    "Apply this setting",
    "应用配置"
  ],
  [
    "Current account",
    "当前账户"
  ],
  [
    "This local process uses the alias chosen in your terminal. Bucket summaries indicate access at the bucket root; storage checks the exact operation and its conditions.",
    "本机进程使用终端选择的别名。桶摘要反映桶根目录权限；具体操作及其条件由存储服务检查。"
  ],
  [
    "New secret key",
    "新的 Secret Key"
  ],
  [
    "Use 8–128 UTF-8 bytes. Your access key stays the same. After rotation, update the alias in your terminal and restart OC console; its old sessions cannot be reused.",
    "使用 8–128 个 UTF-8 字节。Access Key 保持不变。轮换后请更新别名并重启 OC 控制台，旧会话失效。"
  ],
  [
    "Review secret rotation",
    "确认密钥轮换"
  ],
  [
    "Rotate the secret for this process's current IAM identity? This stops the console and revokes all its local sessions. Stop active transfers and deletions before continuing.",
    "轮换当前 IAM 身份的密钥？这将停止控制台并注销所有会话。继续前请停止传输和删除任务。"
  ],
  [
    "Rotate secret and stop console",
    "轮换密钥并停止控制台"
  ],
  [
    "Files are uploaded through the local OC process; credentials remain local.",
    "文件通过本机 OC 进程上传，存储凭据保留在本机。"
  ],
  [
    "Enter a known bucket name directly. Storage validates upload permission.",
    "可以直接输入已知桶名。是否允许上传由存储服务验证。"
  ],
  [
    "Select file",
    "选择上传文件"
  ],
  [
    "Select a file, then confirm the target bucket and object key.",
    "选择一个文件，然后确认目标桶与对象路径。"
  ],
  [
    "Selected file",
    "待上传文件"
  ],
  [
    "Remove",
    "移除"
  ],
  [
    "The file has not been uploaded. Confirm the destination below, then click Upload file.",
    "文件尚未上传。确认下方目标路径后，点击“上传文件”开始传输。"
  ],
  [
    "Slashes, spaces and other characters in the key are preserved. The current prefix is only a default suggestion.",
    "对象路径中的斜线、空格及其他字符会原样保留。当前目录前缀仅作为默认建议。"
  ],
  [
    "Allow replacing an existing object at the same key",
    "允许覆盖相同路径的已有对象"
  ],
  [
    "Only new objects are created by default. Replacing an object requires separate confirmation.",
    "默认仅创建新对象。覆盖已有对象需要单独确认。"
  ],
  [
    "Delete an exact key",
    "按完整路径删除"
  ],
  [
    "Enter the destination directly when bucket browsing is unavailable. The server will prepare one key for review; nothing is deleted yet.",
    "无法浏览桶时可直接输入路径。服务端先准备单个对象的删除计划，此时尚未删除。"
  ],
  [
    "The key is kept exactly as entered. Authorization is checked when deletion executes.",
    "对象路径按输入保留，执行删除时检查权限。"
  ],
  [
    "Prepare deletion plan",
    "准备删除计划"
  ],
  [
    "Confirm replacement",
    "确认覆盖"
  ],
  [
    "This upload may replace the current object or create a new version, depending on storage settings. It does not protect against concurrent changes. Completed writes cannot be undone here.",
    "根据存储设置，上传可能覆盖当前对象或创建新版本，无法防止并发修改。此处无法撤销已完成的写入。"
  ],
  [
    "Replace this object",
    "覆盖此对象"
  ],
  [
    "Confirm object deletion",
    "确认删除对象"
  ],
  [
    "This operation does not undo itself if interrupted. Individual objects can be denied or fail even when a plan succeeds.",
    "中断不会自动撤销操作。即使计划成功，个别对象仍可能被拒绝或失败。"
  ],
  [
    "Delete planned objects",
    "删除计划中的对象"
  ],
  [
    "Type the bucket name to confirm deletion",
    "输入桶名确认删除"
  ],
  [
    "Create private bucket",
    "创建私有桶"
  ],
  [
    "Object version history",
    "对象版本历史"
  ],
  [
    "Choose an exact version to download, share, or include in a ZIP. Delete markers have no downloadable content. Historical versions are never removed here.",
    "选择具体版本进行下载、分享或加入 ZIP。删除标记没有可下载内容。此处不删除历史版本。"
  ],
  [
    "Read versions",
    "读取版本"
  ],
  [
    "Load more versions",
    "加载更多版本"
  ],
  [
    "Share a download",
    "分享下载链接"
  ],
  [
    "Anyone with this link can download until it expires. The storage address must be reachable by the recipient. A link cannot be individually revoked here.",
    "持有链接的人可在到期前下载。接收者必须能访问存储地址。此处无法单独撤销链接。"
  ],
  [
    "Valid for",
    "有效期"
  ],
  [
    "1 hour",
    "1 小时"
  ],
  [
    "15 minutes",
    "15 分钟"
  ],
  [
    "1 day",
    "1 天"
  ],
  [
    "7 days",
    "7 天"
  ],
  [
    "Download filename (optional)",
    "下载文件名（可选）"
  ],
  [
    "Download link",
    "下载链接"
  ],
  [
    "Copy link",
    "复制链接"
  ],
  [
    "Create download link",
    "创建下载链接"
  ],
  [
    "Download as ZIP",
    "下载为 ZIP"
  ],
  [
    "OC prepares a complete ZIP before download.",
    "OC 先准备完整 ZIP，再提供下载。"
  ],
  [
    "Up to 1,000 objects and 5 GiB are supported.",
    "最多支持 1,000 个对象和 5 GiB。"
  ],
  [
    "The ZIP includes a manifest of original keys and versions. If any object fails, no download is offered. Closing this window keeps preparation running; use ZIP task to return. Prepared files expire automatically.",
    "ZIP 包含原始路径和版本清单。任何对象失败均不提供下载。关闭窗口后任务继续，可通过 ZIP 任务返回。文件会自动过期。"
  ],
  [
    "Check ZIP status",
    "检查 ZIP 状态"
  ],
  [
    "Cancel preparation",
    "取消准备"
  ],
  [
    "Prepare ZIP",
    "准备 ZIP"
  ],
  [
    "Download ZIP ↓",
    "下载 ZIP ↓"
  ],
  [
    "Manage users, groups, and service accounts with the current identity. Storage checks each permission. Secret keys are displayed once and cleared when you close this window.",
    "使用当前身份管理用户、组和服务账户，权限由存储服务检查。Secret Key 仅显示一次，关闭窗口后清除。"
  ],
  [
    "View",
    "查看"
  ],
  [
    "Users",
    "用户"
  ],
  [
    "Groups",
    "用户组"
  ],
  [
    "Service accounts",
    "服务账户"
  ],
  [
    "Policies",
    "策略"
  ],
  [
    "Parent user",
    "所属用户"
  ],
  [
    "Refresh list",
    "刷新列表"
  ],
  [
    "Make a change",
    "修改"
  ],
  [
    "Read current policy bindings",
    "读取当前策略绑定"
  ],
  [
    "Type the target name or access key to confirm",
    "输入目标名称或 Access Key 确认"
  ],
  [
    "Apply this change",
    "应用修改"
  ],
  [
    "Save this new credential now",
    "立即保存新凭据"
  ],
  [
    "This is the only display of the returned secret. Keep it in a secure place.",
    "返回的密钥仅显示这一次，请安全保存。"
  ],
  [
    "Access key",
    "Access Key"
  ],
  [
    "Secret key",
    "Secret Key"
  ],
  [
    "Show secret",
    "显示密钥"
  ],
  [
    "I saved it — clear credential",
    "已保存，清除凭据"
  ],
  [
    "Save the new credential",
    "保存新凭据"
  ],
  [
    "Storage confirmed the change and stopped this console connection. Save this new credential now; it will be cleared when you close this window or leave the page. Verify your selected identity and restart OC console before continuing.",
    "存储已确认修改并停止此控制台连接。请立即保存新凭据；关闭窗口或离开页面后会清除。确认身份后重启控制台再继续。"
  ],
  [
    "No favorites yet. Use the star beside a filename to add one.",
    "尚未收藏对象。可通过文件名旁的星标添加。"
  ],
  [
    "Browse a bucket to add a recent visit.",
    "浏览存储桶后显示访问记录。"
  ],
  [
    "Download",
    "下载"
  ],
  [
    "More",
    "更多"
  ],
  [
    "Delete",
    "删除"
  ],
  [
    "Preview",
    "预览"
  ],
  [
    "Details",
    "详情"
  ],
  [
    "Favorite / Unfavorite",
    "收藏 / 取消收藏"
  ],
  [
    "Copy file link",
    "复制文件链接"
  ],
  [
    "Local session download link copied.",
    "已复制本机会话下载链接。"
  ],
  [
    "Copy the selected link above.",
    "请复制上方已选中的链接。"
  ],
  [
    "Unable to load saved preferences. Refresh to retry.",
    "无法读取已保存的偏好，请刷新重试。"
  ],
  [
    "Unable to confirm preferences were saved. Refresh to check.",
    "无法确认偏好已保存，请刷新检查。"
  ],
  [
    "Paste the code from your terminal",
    "粘贴终端中的登录码"
  ],
  [
    "Toggle navigation",
    "展开或收起导航"
  ],
  [
    "OC local console home",
    "OC 本地控制台首页"
  ],
  [
    "Storage browser",
    "存储浏览器"
  ],
  [
    "Console navigation",
    "控制台导航"
  ],
  [
    "File upload",
    "文件上传"
  ],
  [
    "Object deletion",
    "对象删除"
  ],
  [
    "Canceling…",
    "正在取消…"
  ],
  [
    "Cancel task",
    "取消任务"
  ],
  [
    "Checking…",
    "正在检查…"
  ],
  [
    "Check status",
    "检查状态"
  ],
  [
    "Your session has expired.",
    "会话已过期。"
  ],
  [
    "Unable to reach the local OC console. Check that OC is still running.",
    "无法连接本机 OC 控制台，请确认进程仍在运行。"
  ],
  [
    "The local console returned an unexpected response. Try refreshing.",
    "控制台返回了异常响应，请尝试刷新。"
  ],
  [
    "The request could not be completed.",
    "请求未能完成。"
  ],
  [
    "Permission information is loading…",
    "正在加载权限信息…"
  ],
  [
    "Unversioned object",
    "无版本对象"
  ],
  [
    "Download this version ↓",
    "下载此版本 ↓"
  ],
  [
    "Discard ZIP",
    "丢弃 ZIP"
  ],
  [
    "Unnamed record",
    "未命名记录"
  ],
  [
    "No bucket summary is available for this identity. Exact operations may still be permitted.",
    "当前身份没有可用的桶摘要，仍可能允许对指定对象执行操作。"
  ],
  [
    "Read",
    "可读"
  ],
  [
    "Write",
    "可写"
  ],
  [
    "Read not indicated",
    "未标明读取"
  ],
  [
    "Write not indicated",
    "未标明写入"
  ],
  [
    "Summary unavailable",
    "摘要不可用"
  ],
  [
    "Loading…",
    "正在加载…"
  ],
  [
    "Reading image, up to 20 MiB.",
    "正在读取图片，最大支持 20 MiB。"
  ],
  [
    "Only JPEG, PNG, GIF and WebP images up to 20 MiB are supported.",
    "仅支持不超过 20 MiB 的 JPEG、PNG、GIF 和 WebP 图片。"
  ],
  [
    "Unable to read image. Check permission and whether the file exists.",
    "图片读取失败，请确认读取权限及文件是否存在。"
  ],
  [
    "Image exceeds the 20 MiB preview limit. Please download it.",
    "图片超过 20 MiB 预览限制，请使用下载。"
  ],
  [
    "Reading object information…",
    "正在读取对象信息…"
  ],
  [
    "Invalid object information response.",
    "对象信息响应无效。"
  ],
  [
    "Object information loaded from storage.",
    "已读取服务端对象信息。"
  ],
  [
    "Capacity covers only buckets returned by the account summary. Storage controls the refresh interval.",
    "容量仅覆盖账户摘要返回的桶，统计刷新周期由服务端决定。"
  ],
  [
    "Storage capacity summary is unavailable.",
    "服务端容量摘要不可用。"
  ],
  [
    "Read-only mode",
    "只读模式"
  ],
  [
    "Writes enabled",
    "已开启写操作"
  ],
  [
    "Copy local session download link",
    "复制本机会话下载链接"
  ],
  [
    "Version history",
    "历史版本"
  ],
  [
    "File selected. Choose another file or remove it below.",
    "已选择文件，可重新选择或从下方列表移除。"
  ],
  [
    "Selected file removed. Please choose a file.",
    "已移除待上传文件，请重新选择。"
  ],
  [
    "Unable to decode this image. Please download it.",
    "图片格式无法解码，请下载查看。"
  ],
  [
    "Deletion targets the current versions of the listed exact keys. A same-key replacement made after planning may also be deleted. New keys outside this list are not included.",
    "删除计划固定下列对象路径，执行时删除这些路径的当前对象版本。 A same-key replacement made after planning may also be deleted. New keys outside this list are not included."
  ],
  [
    "Your local session has changed. Enter the login code from your OC terminal to reconnect.",
    "本机会话已变化，请输入 OC 终端中的登录码重新连接。"
  ],
  [
    "Your session has expired. Enter the login code from your OC terminal to reconnect.",
    "会话已过期，请输入 OC 终端中的登录码重新连接。"
  ],
  [
    "The local console returned an invalid session.",
    "本机控制台返回了无效会话。"
  ],
  [
    "Read-only browsing.",
    "只读浏览。"
  ],
  [
    "Writes enabled by the local OC process.",
    "本机 OC 进程已启用写入。"
  ],
  [
    "No permission summary is available for this bucket. Browsing remains available.",
    "此桶暂无权限摘要，仍可尝试浏览。"
  ],
  [
    "Read indicated",
    "标明可读"
  ],
  [
    "read indicated",
    "标明可读"
  ],
  [
    "read not indicated",
    "未标明读取"
  ],
  [
    "write indicated",
    "标明可写"
  ],
  [
    "write not indicated",
    "未标明写入"
  ],
  [
    "No objects here",
    "此处没有对象"
  ],
  [
    "This prefix is empty. Try another prefix or return to the root.",
    "当前前缀为空，请尝试其他前缀或返回根目录。"
  ],
  [
    "This bucket is empty.",
    "当前桶为空。"
  ],
  [
    "Loading more objects…",
    "正在加载更多对象…"
  ],
  [
    "Loading objects…",
    "正在加载对象…"
  ],
  [
    "You can choose another bucket or prefix while this request is running.",
    "加载期间可以切换桶或前缀。"
  ],
  [
    "The local console returned an invalid object page.",
    "控制台返回了无效对象列表。"
  ],
  [
    "The server returned a repeated page cursor. Refresh to retry browsing.",
    "服务端返回了重复分页游标，请刷新重试。"
  ],
  [
    "Access restricted",
    "访问受限"
  ],
  [
    "Objects unavailable",
    "对象不可用"
  ],
  [
    "You do not have permission to list this location. Try a permitted prefix.",
    "你没有列出此位置的权限，请尝试允许访问的前缀。"
  ],
  [
    "Check the local OC process and try again.",
    "请检查本机 OC 进程后重试。"
  ],
  [
    "Loading buckets…",
    "正在加载存储桶…"
  ],
  [
    "The local console returned an invalid bucket list.",
    "控制台返回了无效存储桶列表。"
  ],
  [
    "No buckets available",
    "没有可用存储桶"
  ],
  [
    "No buckets were returned for the selected alias and identity.",
    "当前别名及身份未返回存储桶。"
  ],
  [
    "Buckets unavailable",
    "存储桶不可用"
  ],
  [
    "Use Refresh to retry the bucket list. OC uses the identity configured for this alias.",
    "点击刷新重试桶列表，OC 使用此别名配置的身份。"
  ],
  [
    "Permission summary unavailable.",
    "权限摘要不可用。"
  ],
  [
    "Permission summary unavailable. Bucket browsing and downloads remain available.",
    "权限摘要不可用，仍可尝试浏览和下载。"
  ],
  [
    "The local session has changed.",
    "本机会话已变化。"
  ],
  [
    "Allow pop-ups for this local console, then try the download again.",
    "请允许本机控制台弹出窗口后重试下载。"
  ],
  [
    "Download requested. If the object is unavailable or access is denied, its error appears in the new tab.",
    "已请求下载；若对象不可用或无访问权限，错误将在新标签页显示。"
  ],
  [
    "The console returned an invalid task status. Check the object list before retrying a write.",
    "控制台返回了无效任务状态，重试写入前请检查对象列表。"
  ],
  [
    "The task record has expired or was reclaimed. Its final outcome is unavailable here; check storage before retrying any write.",
    "任务记录已过期或回收，最终结果不可用；重试写入前请检查存储。"
  ],
  [
    "Task status could not be read.",
    "无法读取任务状态。"
  ],
  [
    "Task status is temporarily unavailable. Use Refresh or Check status to retry; no write will be retried automatically.",
    "任务状态暂不可用，点击刷新或检查状态重试；写入不会自动重试。"
  ],
  [
    "Task status is temporarily unavailable. No write will be retried automatically.",
    "任务状态暂不可用，写入不会自动重试。"
  ],
  [
    "Task record expired · outcome unavailable",
    "任务记录已过期 · 结果不可用"
  ],
  [
    "Waiting for file",
    "等待文件"
  ],
  [
    "Building deletion plan",
    "正在准备删除计划"
  ],
  [
    "Awaiting confirmation",
    "等待确认"
  ],
  [
    "Partially completed",
    "部分完成"
  ],
  [
    "File sent · awaiting storage result",
    "文件已发送 · 等待存储结果"
  ],
  [
    "Sending file to OC",
    "正在向 OC 发送文件"
  ],
  [
    "Unknown status",
    "未知状态"
  ],
  [
    "File transfer from browser to OC; storage completion is shown separately",
    "文件从浏览器传输至 OC，存储完成状态单独显示"
  ],
  [
    "Replacement allowed",
    "允许覆盖"
  ],
  [
    "Create only",
    "仅创建"
  ],
  [
    "This page has no active file transfer for this task. Cancel it before preparing a new upload.",
    "当前页面没有此任务的活动传输，请取消后再准备新上传。"
  ],
  [
    "The object may already have been saved. Refresh and check it before retrying.",
    "对象可能已保存，请刷新检查后再重试。"
  ],
  [
    "The original confirmation is unavailable on this page. Cancel this plan and create a fresh one to review its scope.",
    "当前页面没有原确认信息，请取消计划并重新创建以检查范围。"
  ],
  [
    "Completed deletions were not undone. Pending objects may remain.",
    "已完成的删除未被撤销，待处理对象可能仍存在。"
  ],
  [
    "Last known object results",
    "最后已知对象结果"
  ],
  [
    "Object results",
    "对象结果"
  ],
  [
    "Cancellation requested. Completed work is not rolled back; an in-flight result may be unconfirmed.",
    "已请求取消。已完成操作不会回滚，进行中的结果可能无法确认。"
  ],
  [
    "Preparing upload… No file bytes have been sent yet.",
    "正在准备上传… 尚未发送文件内容。"
  ],
  [
    "Preparing upload…",
    "正在准备上传…"
  ],
  [
    "Upload preparation did not match the requested destination. No file bytes were sent.",
    "上传准备结果与目标不匹配，尚未发送文件内容。"
  ],
  [
    "Your session has expired or changed.",
    "会话已过期或变化。"
  ],
  [
    "The upload result could not be read.",
    "无法读取上传结果。"
  ],
  [
    "File transfer was interrupted. The storage result may be unconfirmed; check task status and the object before retrying.",
    "文件传输中断，存储结果可能无法确认；重试前请检查任务及对象。"
  ],
  [
    "This browser could not start the file transfer. Cancel this waiting task before preparing a new upload.",
    "浏览器无法开始传输，请取消等待任务后再准备新上传。"
  ],
  [
    "Building a fixed object list… Nothing is being deleted.",
    "正在固定对象列表… 尚未删除任何对象。"
  ],
  [
    "The server did not return a complete deletion plan. Nothing was executed. Cancel the task and create a fresh plan.",
    "服务端未返回完整删除计划，尚未执行。请取消并重新创建。"
  ],
  [
    "The deletion plan does not match the requested scope. Nothing was executed. Cancel the task and create a fresh plan.",
    "删除计划与请求范围不匹配，尚未执行。请取消并重新创建。"
  ],
  [
    "Explicitly selected object keys",
    "明确选中的对象路径"
  ],
  [
    "at an unknown time",
    "时间未知"
  ],
  [
    "This plan has expired. Close it and create a fresh plan to review the current scope.",
    "计划已过期，请关闭并重新创建以检查当前范围。"
  ],
  [
    "This plan can no longer be executed here. Check its task status or create a fresh plan.",
    "此计划无法继续执行，请检查任务状态或重新创建。"
  ],
  [
    "Review every listed key. Confirming deletes their current objects; this cannot be undone here.",
    "请检查每个对象路径，确认后删除当前对象，此处无法撤销。"
  ],
  [
    "The plan contains no objects. Confirming completes an empty task without deleting anything.",
    "计划没有对象，确认仅完成空任务，不会删除内容。"
  ],
  [
    "Complete empty plan",
    "完成空计划"
  ],
  [
    "Submitting this exact plan… Check the task panel for per-object results.",
    "正在提交当前计划… 各对象结果见任务面板。"
  ],
  [
    "The server returned a different deletion task. Check task status before doing anything else.",
    "服务端返回了不同删除任务，请先检查任务状态。"
  ],
  [
    "Execution was rejected because other write tasks are active. This plan has not started.",
    "其他写入任务正在运行，本次执行被拒绝，计划尚未开始。"
  ],
  [
    "Other write tasks are active. This plan has not started. Wait for a task to finish, then confirm this same plan again.",
    "其他写入任务正在运行，计划尚未开始。请等待任务完成后重新确认。"
  ],
  [
    "The task record has expired or was reclaimed. Its outcome is unavailable; check storage before preparing another deletion.",
    "任务记录已过期或回收，结果不可用；再次准备删除前请检查存储。"
  ],
  [
    "Enter a bucket and choose a setting to read. Each setting has its own storage permission.",
    "输入桶名并选择配置项，每项配置分别检查权限。"
  ],
  [
    "The local console returned an invalid setting. No change can be submitted.",
    "控制台返回了无效配置，无法提交修改。"
  ],
  [
    "configuration present",
    "已有配置"
  ],
  [
    "no stored configuration",
    "未保存配置"
  ],
  [
    "This console is read-only.",
    "当前控制台为只读模式。"
  ],
  [
    "The server did not provide conditional update support and a valid revision. Changes are unavailable.",
    "服务端未提供条件更新支持及有效修订号，无法修改。"
  ],
  [
    "Saving requires this exact configuration revision; a concurrent change is rejected.",
    "保存要求配置修订号一致，并发修改将被拒绝。"
  ],
  [
    "The previous configuration and draft are retained.",
    "原配置和草稿已保留。"
  ],
  [
    "Enter the complete configuration, or use Review removal for a stored policy or lifecycle.",
    "输入完整配置，或确认移除已有策略、生命周期配置。"
  ],
  [
    "A bucket policy can expose objects publicly or restrict future access. Review every statement and condition before applying it.",
    "桶策略可能公开对象或限制后续访问，请检查每项声明及条件。"
  ],
  [
    "Versioning affects future writes and deletes. Enabling it can increase storage use; suspension keeps previous versions and cannot disable object-lock requirements.",
    "版本控制影响后续写入和删除，开启可能增加用量；暂停会保留历史版本，无法解除对象锁要求。"
  ],
  [
    "Lifecycle rules can expire current objects and permanently delete historical versions later. Removing rules stops those rules from scheduling future work; it cannot restore objects already removed.",
    "生命周期规则可使当前对象过期或永久删除历史版本。移除规则仅停止后续调度，无法恢复已删除对象。"
  ],
  [
    "Remove the complete stored configuration",
    "移除完整已存配置"
  ],
  [
    "Replace the complete configuration",
    "替换完整配置"
  ],
  [
    "Remove this configuration.",
    "移除此配置。"
  ],
  [
    "Remove this setting",
    "移除此配置项"
  ],
  [
    "The save response was interrupted. Its outcome is unknown; read the setting before making another change.",
    "保存响应中断，结果未知；再次修改前请读取配置。"
  ],
  [
    "Applying the reviewed setting…",
    "正在应用已确认配置…"
  ],
  [
    "The local console returned an unexpected update response. Its outcome is unknown.",
    "控制台返回了异常更新响应，结果未知。"
  ],
  [
    "The storage server confirmed this change. Read again before editing if the configuration changed elsewhere.",
    "存储服务已确认修改；若其他位置更改过配置，请重新读取。"
  ],
  [
    "The configuration changed since it was read. Your draft is retained. Copy any edits you want to keep, then read the current setting and review a fresh change.",
    "配置自读取后已变化，草稿已保留。请复制需保留的编辑，再读取当前配置并重新确认。"
  ],
  [
    "The update outcome is unknown. Your draft is retained. Read the setting to check what storage accepted before making another change; this write will not be replayed.",
    "更新结果未知，草稿已保留。再次修改前请读取配置确认结果，此写入不会重放。"
  ],
  [
    "Loading the current identity's account information…",
    "正在加载当前身份的账户信息…"
  ],
  [
    "Current identity information is unavailable. Secret rotation is disabled.",
    "当前身份信息不可用，密钥轮换已禁用。"
  ],
  [
    "Root account",
    "根账户"
  ],
  [
    "IAM user",
    "IAM 用户"
  ],
  [
    "Temporary STS identity",
    "临时 STS 身份"
  ],
  [
    "Service account",
    "服务账户"
  ],
  [
    "Directory identity",
    "目录身份"
  ],
  [
    "Identity type unavailable",
    "身份类型不可用"
  ],
  [
    "status unavailable",
    "状态不可用"
  ],
  [
    "This local console is read-only. Secret rotation is disabled.",
    "本机控制台为只读模式，密钥轮换已禁用。"
  ],
  [
    "This server does not provide safe current-identity discovery. Secret rotation is unavailable; bucket browsing remains independent.",
    "服务端不支持安全识别当前身份，无法轮换密钥；桶浏览不受影响。"
  ],
  [
    "This identity cannot rotate its secret here. Manage root, temporary, service, and directory credentials through their respective owner or identity provider.",
    "此身份无法在这里轮换密钥。根账户、临时、服务及目录凭据需通过对应所有者或身份提供方管理。"
  ],
  [
    "The storage server does not permit secret rotation for this identity.",
    "存储服务不允许此身份轮换密钥。"
  ],
  [
    "Secret rotation changes only this IAM user's secret and preserves its account status. OC does not write the new secret to your alias file.",
    "轮换仅修改此 IAM 用户密钥，保留账户状态。OC 不将新密钥写入别名文件。"
  ],
  [
    "Account information is unavailable. Bucket browsing remains independent; secret rotation is disabled.",
    "账户信息不可用，桶浏览不受影响，密钥轮换已禁用。"
  ],
  [
    "Current identity information could not be refreshed; secret rotation is disabled.",
    "无法刷新当前身份信息，密钥轮换已禁用。"
  ],
  [
    "The storage server confirmed secret rotation. All local sessions have ended. Update this alias's secret in your terminal and restart OC console.",
    "存储服务已确认密钥轮换，所有会话已结束。请在终端更新别名密钥并重启 OC 控制台。"
  ],
  [
    "The secret rotation outcome is unknown. This console has stopped using the old identity. Verify which secret works in your terminal, update the alias, and restart OC console. Do not repeat this request automatically.",
    "密钥轮换结果未知，控制台已停止使用旧身份。请在终端确认有效密钥、更新别名并重启。请勿自动重复请求。"
  ],
  [
    "Enter a new secret containing 8–128 UTF-8 bytes.",
    "请输入 8–128 个 UTF-8 字节的新密钥。"
  ],
  [
    "Rotating the secret and stopping this console…",
    "正在轮换密钥并停止控制台…"
  ],
  [
    "Stop active transfers and deletions before rotating the secret. Enter the new secret again when they finish.",
    "轮换前请停止传输及删除，任务结束后重新输入新密钥。"
  ],
  [
    "Enter an exact known bucket name, even if listing buckets is not allowed. Only an empty bucket can be deleted. Objects, historical versions, and delete markers must be removed separately. OC never empties the bucket automatically.",
    "可直接输入已知桶名。仅能删除空桶；对象、历史版本和删除标记需单独移除，OC 不自动清空桶。"
  ],
  [
    "A new bucket starts private. Use 3–63 lowercase letters, digits, dots, or hyphens; begin and end with a letter or digit.",
    "新桶默认私有。名称使用 3–63 个小写字母、数字、点或连字符，首尾必须为字母或数字。"
  ],
  [
    "Delete this empty bucket",
    "删除此空桶"
  ],
  [
    "Enter the exact bucket name to confirm deletion.",
    "请输入准确桶名以确认删除。"
  ],
  [
    "Deleting empty bucket…",
    "正在删除空桶…"
  ],
  [
    "Creating bucket…",
    "正在创建存储桶…"
  ],
  [
    "Loading versions…",
    "正在加载版本…"
  ],
  [
    "The console returned an invalid version list.",
    "控制台返回了无效版本列表。"
  ],
  [
    "Latest ·",
    "最新 ·"
  ],
  [
    "Share this version",
    "分享此版本"
  ],
  [
    "ZIP this version",
    "此版本加入 ZIP"
  ],
  [
    "Current object",
    "当前对象"
  ],
  [
    "Creating a download link…",
    "正在创建下载链接…"
  ],
  [
    "The console returned an invalid share link.",
    "控制台返回了无效分享链接。"
  ],
  [
    "Link copied.",
    "链接已复制。"
  ],
  [
    "Link selected. Copy it using your browser or keyboard.",
    "链接已选中，请使用浏览器或键盘复制。"
  ],
  [
    "(all objects)",
    "（全部对象）"
  ],
  [
    "The selected keys are fixed. Current objects are resolved during preparation.",
    "选中路径已固定，准备期间解析当前对象。"
  ],
  [
    "Planning archive…",
    "正在规划压缩包…"
  ],
  [
    "Preparing ZIP…",
    "正在准备 ZIP…"
  ],
  [
    "ZIP ready to download",
    "ZIP 已准备好下载"
  ],
  [
    "Downloading ZIP…",
    "正在下载 ZIP…"
  ],
  [
    "ZIP download completed",
    "ZIP 下载完成"
  ],
  [
    "Archive preparation failed",
    "压缩包准备失败"
  ],
  [
    "Archive canceled",
    "压缩包已取消"
  ],
  [
    "Archive preparation failed.",
    "压缩包准备失败。"
  ],
  [
    "The console returned an invalid archive status.",
    "控制台返回了无效压缩包状态。"
  ],
  [
    "The console returned an invalid ZIP task list.",
    "控制台返回了无效 ZIP 任务列表。"
  ],
  [
    "Archive expired or is no longer available.",
    "压缩包已过期或不可用。"
  ],
  [
    "Planning ZIP…",
    "正在规划 ZIP…"
  ],
  [
    "Archive expired or is no longer available. Close this window and prepare a new ZIP.",
    "压缩包已过期或不可用，请关闭窗口并重新准备。"
  ],
  [
    "Change user policy bindings",
    "修改用户策略绑定"
  ],
  [
    "Change group policy bindings",
    "修改用户组策略绑定"
  ],
  [
    "Create user",
    "创建用户"
  ],
  [
    "Enable user",
    "启用用户"
  ],
  [
    "Disable user",
    "禁用用户"
  ],
  [
    "Change user secret key",
    "修改用户密钥"
  ],
  [
    "Delete user",
    "删除用户"
  ],
  [
    "Create empty group",
    "创建空用户组"
  ],
  [
    "Add group members",
    "添加组成员"
  ],
  [
    "Remove group members",
    "移除组成员"
  ],
  [
    "Enable group",
    "启用用户组"
  ],
  [
    "Disable group",
    "禁用用户组"
  ],
  [
    "Delete empty group",
    "删除空用户组"
  ],
  [
    "Create service account",
    "创建服务账户"
  ],
  [
    "Enable service account",
    "启用服务账户"
  ],
  [
    "Disable service account",
    "禁用服务账户"
  ],
  [
    "Change service account secret key",
    "修改服务账户密钥"
  ],
  [
    "Change service account restrictions",
    "修改服务账户限制"
  ],
  [
    "Delete service account",
    "删除服务账户"
  ],
  [
    "User access key",
    "用户 Access Key"
  ],
  [
    "New secret key (optional; generated if empty)",
    "新 Secret Key（可选，留空自动生成）"
  ],
  [
    "Group name",
    "用户组名称"
  ],
  [
    "Member access keys (one per line)",
    "成员 Access Key（每行一个）"
  ],
  [
    "Parent user access key",
    "所属用户 Access Key"
  ],
  [
    "New access key (optional)",
    "新 Access Key（可选）"
  ],
  [
    "Restriction policy JSON (optional)",
    "限制策略 JSON（可选）"
  ],
  [
    "Service account access key",
    "服务账户 Access Key"
  ],
  [
    "Restriction policy JSON",
    "限制策略 JSON"
  ],
  [
    "Complete policy bindings (one policy name per line; empty removes all bindings)",
    "完整策略绑定（每行一个策略名，留空移除全部绑定）"
  ],
  [
    "Confirm the exact target before applying. Changes are never retried automatically. Policy binding changes require a fresh read and support for safe concurrent updates.",
    "应用前请确认准确目标。修改不自动重试。修改策略绑定需要重新读取及安全并发更新支持。"
  ],
  [
    "Remove listed service accounts and group memberships first. Current membership is checked again before deletion. Deletion revokes temporary credentials and direct policy bindings, and may revoke service credentials or memberships created concurrently.",
    "请先移除服务账户及组成员关系。删除前会再次检查成员关系。删除会撤销临时凭据和直接策略绑定，也可能撤销并发创建的服务凭据或成员关系。"
  ],
  [
    "Only an empty group can be deleted.",
    "仅能删除空用户组。"
  ],
  [
    "Confirm the parent user's access key. Supply an access key when you supply a secret key; leave both blank to generate a credential. Save the returned credential before closing.",
    "请确认所属用户 Access Key。提供 Secret Key 时需同时提供 Access Key；均留空自动生成。关闭前保存返回凭据。"
  ],
  [
    "Existing clients must update their credentials after the change.",
    "修改后现有客户端需更新凭据。"
  ],
  [
    "Enter an exact parent user to list its service accounts.",
    "输入准确所属用户以列出服务账户。"
  ],
  [
    "Loading access records…",
    "正在加载访问记录…"
  ],
  [
    "The console returned invalid access records.",
    "控制台返回了无效访问记录。"
  ],
  [
    "Choose for change",
    "选择并修改"
  ],
  [
    "Enter an access key when supplying a secret key, or leave both blank to generate the credential.",
    "提供 Secret Key 时请输入 Access Key，或均留空自动生成。"
  ],
  [
    "Enter the exact target name or access key to confirm this change.",
    "请输入准确目标名称或 Access Key 确认修改。"
  ],
  [
    "Applying the change…",
    "正在应用修改…"
  ],
  [
    "The change was not confirmed. Verify storage before retrying.",
    "修改结果未确认，重试前请检查存储。"
  ],
  [
    "The current credentials changed. Update the alias in your terminal and restart OC console before continuing.",
    "当前凭据已变化，请在终端更新别名并重启 OC 控制台。"
  ],
  [
    "Change confirmed by storage.",
    "存储服务已确认修改。"
  ],
  [
    "Change confirmed. Read current bindings before making another change.",
    "修改已确认，再次修改前请读取当前绑定。"
  ],
  [
    "The credential change outcome is unconfirmed. Verify which secret works, update the alias, and restart OC console. This action will not be repeated.",
    "凭据修改结果未确认。请确认有效密钥、更新别名并重启 OC 控制台，此操作不会重复。"
  ],
  [
    "Read current bindings before another change. Your policy draft is preserved; no conflicting change is retried automatically.",
    "再次修改前请读取当前绑定。策略草稿已保留，冲突修改不会自动重试。"
  ],
  [
    "Enter an exact user or group before reading its policy bindings.",
    "读取策略绑定前请输入准确用户或组。"
  ],
  [
    "Reading current policy bindings…",
    "正在读取当前策略绑定…"
  ],
  [
    "The console returned invalid policy bindings.",
    "控制台返回了无效策略绑定。"
  ],
  [
    "Edit the complete list. Saving checks that these bindings have not changed since this read.",
    "编辑完整列表，保存时检查绑定自读取后是否变化。"
  ],
  [
    "Current storage permits reading these bindings. Editing requires support for safe concurrent changes.",
    "当前存储允许读取绑定，编辑需要安全并发修改支持。"
  ],
  [
    "Hide secret",
    "隐藏密钥"
  ],
  [
    "You are logged out. Use the code from your OC terminal to reconnect.",
    "已退出登录，请使用 OC 终端中的登录码重新连接。"
  ],
  [
    "Choose a file and enter an exact bucket and full object key.",
    "请选择文件并输入准确桶名及完整对象路径。"
  ],
  [
    "Upload preparation was stopped before any file bytes were sent.",
    "上传准备已停止，尚未发送文件内容。"
  ],
  [
    "Running",
    "运行中"
  ],
  [
    "Succeeded",
    "已成功"
  ],
  [
    "Failed",
    "失败"
  ],
  [
    "Canceled",
    "已取消"
  ],
  [
    "Delete marker",
    "删除标记"
  ],
  [
    "Storage overview",
    "存储概览"
  ],
  [
    "Read-only browsing. Downloads use your browser’s download manager. Download errors open in a separate tab.",
    "只读浏览。下载由浏览器管理，下载错误在新标签页显示。"
  ],
  [
    "Writes enabled by the local OC process. Downloads use your browser’s download manager. Download errors open in a separate tab.",
    "本机 OC 进程已开启写入。下载由浏览器管理，下载错误在新标签页显示。"
  ]
];
 const english = new Map(pairs.map(([en,zh]) => [zh,en]));
 const chinese = new Map(pairs);
 let language = "zh";
 const sources = new WeakMap();
 const attributes = new WeakMap();
 const excluded = "[data-no-i18n],pre,code,textarea,#console-language,#alias-name,#object-info-key,#object-info-bucket,#object-info-etag,#object-info-type,#image-preview-title,#upload-selected-name,#upload-bucket,#replace-key,#replace-bucket,#delete-items,#archive-items,#iam-result-access,#iam-result-secret,#stopped-result-access,#stopped-result-secret";
 function translate(text) {
   const leading = text.match(/^\s*/)[0], trailing = text.match(/\s*$/)[0];
   const trimmed = text.trim();
   if (!trimmed) return text;
   const dict = language === "en" ? english : chinese;
   let output = dict.get(trimmed);
   if (!output) {
     let match;
     const labelPrefixes = [["Rename ","重命名 "],["Review deletion of ","确认删除 "],["Open favorite ","打开收藏 "],["Remove favorite ","移除收藏 "],["Copy file link ","复制文件链接 "],["Favorite ","收藏 "],["Preview image ","预览图片 "],["Object details ","文件详情 "],["Download ","下载 "],["Select ","选择 "],["Read versions of ","读取历史版本 "],["Share ","分享 "]];
     for (const [en,zh] of labelPrefixes) {
       const from=language === "en" ? zh : en, to=language === "en" ? en : zh;
       if (trimmed.startsWith(from)) { output=to+trimmed.slice(from.length); break; }
     }
     if (language === "en" && (match=trimmed.match(/^显示 (\d+) 个桶 \/ 共 (\d+) 个(.*)$/))) output=`Showing ${match[1]} of ${match[2]} buckets${match[3] ? " · No matching buckets" : ""}`;
     if (language === "zh" && (match=trimmed.match(/^(\d+) entr(?:y|ies) loaded$/))) output=`已加载 ${match[1]} 个对象`;
     if (language === "zh" && (match=trimmed.match(/^(\d+) loaded objects? selected$/))) output=`已选择 ${match[1]} 个已加载对象`;
     if (language === "zh" && (match=trimmed.match(/^Delete (\d+) planned objects?$/))) output=`删除计划中的 ${match[1]} 个对象`;
     if (language === "zh" && (match=trimmed.match(/^Maximum file size: (.*)\.$/))) output=`最大文件大小：${match[1]}。`;
     if (language === "zh" && (match=trimmed.match(/^Created (.*)$/))) output=`创建于 ${match[1]}`;

     const templates = [
       [/^Saving requires this exact configuration revision; a concurrent change is rejected\.(.*)$/, m => `保存时将校验当前配置版本，并发修改会被拒绝。${translate(m[1])}`],
       [/^No stored configuration is present\. Enter the complete (JSON|XML) document to create one\.$/, m => `当前未保存配置，请输入完整的 ${m[1]} 文档以创建配置。`],
       [/^(Replace|Remove) the complete (?:stored )?configuration for this exact bucket\. (.*)$/, m => `${m[1] === "Replace" ? "替换" : "移除"}此目标桶的完整配置。${translate(m[2])}`],

       [/^(\d+) of (\d+) objects attempted; (\d+) succeeded\.$/, m => `已尝试 ${m[1]} / ${m[2]} 个对象，成功 ${m[3]} 个。`],
       [/^(\d+)% sent from this browser; this alone does not confirm storage success\.$/, m => `浏览器已发送 ${m[1]}%，尚不能确认存储成功。`],
       [/^Last known server status: (.*)\.$/, m => `最后已知服务端状态：${translate(m[1])}。`],
       [/^(Last known object results|Object results) \((\d+)\)$/, m => `${m[1] === "Object results" ? "对象结果" : "最后已知对象结果"}（${m[2]}）`],
       [/^(\d+) explicitly selected object keys$/, m => `已明确选择 ${m[1]} 个对象路径`],
       [/^Exact prefix: (.*)$/, m => `完整前缀：${m[1]}`],
       [/^Loaded (user|group) (.*)$/, m => `已读取${m[1] === "user" ? "用户" : "组"} ${m[2]}`],
       [/^Read bindings for (user|group) (.*)\? A successful read will replace the unsaved policy list\. A failed read keeps the draft\.$/, m => `读取${m[1] === "user" ? "用户" : "组"} ${m[2]} 的绑定关系？成功读取将替换未保存的策略列表，失败时保留草稿。`],
       [/^(\d+) objects selected$/, m => `已选择 ${m[1]} 个对象`],
       [/^Up to (.*) objects and (.*) are supported\.$/, m => `最多支持 ${m[1]} 个对象和 ${m[2]}。`],
       [/^(.*) selected\. Maximum file size: (.*)\.$/, m => `已选择 ${m[1]}。最大文件大小：${m[2]}。`],
       [/^The selected file exceeds this console’s (.*) upload limit\.$/, m => `所选文件超过此控制台的 ${m[1]} 上传限制。`],
       [/^Expires (.*)\. Temporary credentials may expire sooner\.$/, m => `到期时间：${m[1]}。临时凭据可能更早失效。`],
       [/^(\d+) selected objects?$/, m => `已选择 ${m[1]} 个对象`],
       [/^(\d+) objects$/, m => `${m[1]} 个对象`],
       [/^Latest$/, () => "最新版本"],
       [/^Recovered ZIP task$/, () => "已恢复 ZIP 任务"],
       [/^(Groups|Listed groups|Parent|Policies|Members): (.*)$/, m => `${({Groups:"所属组","Listed groups":"已列出组",Parent:"父用户",Policies:"策略",Members:"成员"})[m[1]]}：${m[2]}`],
       [/^(\d+) (.*) returned\. Policy bindings can be inspected through the policy binding action\.$/, m => `已返回 ${m[1]} 条${({users:"用户",groups:"组",policies:"策略","service accounts":"服务账户"})[m[2]] || m[2]}记录。可通过策略绑定操作查看绑定关系。`],
     ];
     if (!output && language === "zh") for (const [pattern, render] of templates) { const found = trimmed.match(pattern); if (found) { output = render(found); break; } }
     // Translate structured UI summaries without rewriting unknown segments.
     if (!output && trimmed.includes(" · ")) output=trimmed.split(" · ").map(part=>translate(part)).join(" · ");
   }
   return leading + (output || trimmed) + trailing;
 }
 function refresh(root = document.body) {
   if (!root || !document.createTreeWalker) return;
   const walker = document.createTreeWalker(root, 4);
   let node;
   while ((node = walker.nextNode())) {
     if (!node.parentElement || node.parentElement.closest(excluded)) continue;
     const old = sources.get(node);
     const source = old && (node.nodeValue === old.output || node.nodeValue === old.source) ? old.source : node.nodeValue;
     const output = translate(source);
     sources.set(node,{source,output});
     if (node.nodeValue !== output) node.nodeValue = output;
   }
   for (const element of root.querySelectorAll("[placeholder],[aria-label],[title]")) {
     if (element.closest(excluded)) continue;
     const saved = attributes.get(element) || {};
     for (const attr of ["placeholder","aria-label","title"]) {
       const value = element.getAttribute(attr); if (value === null) continue;
       const old = saved[attr];
       const source = old && (value === old.output || value === old.source) ? old.source : value;
       const output = translate(source); saved[attr] = {source,output};
       if (value !== output) element.setAttribute(attr,output);
     }
     attributes.set(element,saved);
   }
 }
 function setLanguage(value) {
   if (value !== "en" && value !== "zh") return;
   language = value; document.title = value === "zh" ? "OC · 本地控制台" : "OC · Local console"; document.documentElement.lang = value === "zh" ? "zh-CN" : "en";
   const select = document.getElementById("console-language"); if(select) select.value = value;
   refresh();
 }
 window.OCI18n = {setLanguage,translate,refresh,language:()=>language};
 const select = document.getElementById("console-language");
 if (select) select.addEventListener("change", () => {setLanguage(select.value);window.dispatchEvent(new CustomEvent("oc-language-change",{detail:language}));});
 setLanguage(language);
 if (typeof MutationObserver !== "undefined") {
   new MutationObserver(() => refresh()).observe(document.body,{childList:true,subtree:true,characterData:true,attributes:true,attributeFilter:["placeholder","aria-label","title"]});
 }
})();
