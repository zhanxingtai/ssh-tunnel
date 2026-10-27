package i18n

// catalogZh holds the Simplified Chinese messages.
var catalogZh = catalog{
	// 通用
	"usage.text": `ssh-tunnel — SSH 端口映射与 SOCKS5 代理管理工具

用法:
  ssh-tunnel                     打开图形化管理界面（默认）
  ssh-tunnel run <隧道>          前台运行指定隧道
  ssh-tunnel daemon [--all] [隧道...]  后台运行隧道（无界面，供自启动使用）
  ssh-tunnel tray               驻留系统托盘（Windows）
  ssh-tunnel autostart <on|off|status> [方式]
  ssh-tunnel service <start|stop|restart|status> [方式]
  ssh-tunnel config <path|list|show|host|tunnel> ...
  ssh-tunnel version

配置管理：
  ssh-tunnel config list                    列出所有主机与隧道
  ssh-tunnel config show <名称或标识>        显示主机或隧道（JSON）
  ssh-tunnel config host add [显示名] [选项] 新增主机
  ssh-tunnel config host edit <名称> [选项]  修改主机
  ssh-tunnel config host remove <名称>       删除主机（及其隧道）
  ssh-tunnel config tunnel add [显示名] [选项]  新增隧道
  ssh-tunnel config tunnel edit <名称> [选项]    修改隧道
  ssh-tunnel config tunnel remove <名称>       删除隧道

主机选项：
  --name <显示名> --host <主机> --port <端口> --user <用户>
  --auth <key|password|agent> --key <私钥路径> --password <密码>
  --passphrase <私钥口令> --auto-reconnect --no-auto-reconnect
  --insecure-host-key

凭据的读取方式（避免密码出现在进程列表中）：
  --password -            从标准输入读取密码
  --password-env <变量>   从环境变量读取密码
  --passphrase -          从标准输入读取私钥口令
  --passphrase-env <变量> 从环境变量读取私钥口令

隧道选项：
  --name <显示名> --host <所属主机> --type <local|remote|socks5>
  --listen <监听地址> --target <目标地址> --auto-start --no-auto-start

隧道类型：
  local   本地转发（远端端口 → 本地）
  remote  远程转发（本地端口 → 远端）
  socks5  动态代理（SOCKS5）

通用参数：
  --config <文件>

自启动方式（Linux）:
  desktop        XDG 桌面自启动，需要桌面会话
  systemd-user   systemd 用户服务，需 loginctl enable-linger 才能开机自启
  systemd-system systemd 系统服务，需要管理员权限
  cron           crontab 的 @reboot
省略方式时按当前环境自动选择。

全局参数:
  --config <文件>   指定配置文件（默认：~/.config/ssh-tunnel/config.json）
  --addr <地址>     图形界面监听地址（默认 127.0.0.1:8787）
  --lang <语言>     界面与输出的语言（zh-CN 或 en-US）
  --no-open         不自动打开窗口或浏览器`,

	"err.unknown_command":   "未知命令 %q",
	"err.flag_config_value": "--config 需要一个值",
	"err.flag_addr_value":   "--addr 需要一个值",
	"err.unknown_language":  "不支持的语言 %q，可用：zh-CN、en-US",
	"err.unknown_theme":     "不支持的主题 %q，可用：auto、dark、light",

	"msg.error_line":  "错误：%v",
	"msg.lang_option": "--lang",

	// 主命令
	"err.run_requires_tunnel":  "run 需要一个隧道名称",
	"err.tunnel_not_found":     "找不到隧道 %q",
	"msg.tunnel_started_ctrlc": "隧道 %s 已启动，按 Ctrl+C 停止",
	"msg.stopping":             "正在停止…",
	"msg.gui_address":          "图形界面：",
	"msg.open_manually":        "请手动打开：",
	"msg.already_running_at":   "已在运行：",
	"msg.no_tunnel_configured": "尚未配置隧道，等待新增",
	"msg.cannot_start":         "无法启动 %s：%v",
	"msg.tunnel_started":       "隧道 %s 已启动",

	// 自启动
	"err.autostart_requires_action": "autostart 需要 on、off 或 status（方式：%v）",
	"err.unknown_autostart_action":  "未知的自启动操作 %q",
	"msg.autostart_enabled":         "已启用自启动（%s）",
	"msg.autostart_disabled":        "已禁用自启动（%s）",
	"msg.autostart_start_command":   "启动命令：",
	"msg.linger_note":               "提示：启用 linger 才能在登录前启动：loginctl enable-linger %s",
	"msg.autostart_status_enabled":  "已启用（%s）：%s",
	"msg.autostart_status_disabled": "已禁用（%s）",

	"msg.auto_start_marker": "  自动启动",
	"msg.dynamic_target":    "（动态）",
	// 配置命令行
	"err.config_requires_action":  "config 需要以下之一：path、list、show、host、tunnel",
	"err.unknown_config_action":   "未知的 config 操作 %q",
	"msg.no_host_configured":      "尚未配置主机",
	"err.show_requires_name":      "show 需要主机或隧道的名称",
	"err.neither_host_nor_tunnel": "%q 既不是主机也不是隧道",
	"err.host_requires_action":    "host 需要以下之一：add、edit、remove",
	"err.unknown_host_action":     "未知的 host 操作 %q",
	"err.edit_requires_host":      "edit 需要主机名称或标识",
	"err.host_exists":             "主机 %s 已存在，请直接编辑现有主机",
	"err.host_not_found":          "找不到主机 %q",
	"err.remove_requires_host":    "remove 需要主机名称或标识",
	"msg.host_added":              "已添加主机 %s（%s）",
	"msg.host_updated":            "已更新主机 %s（%s）",
	"msg.host_removed":            "已删除主机 %s（%s）",
	"err.tunnel_requires_action":  "tunnel 需要以下之一：add、edit、remove",
	"err.unknown_tunnel_action":   "未知的 tunnel 操作 %q",
	"err.edit_requires_tunnel":    "edit 需要隧道名称或标识",
	"err.remove_requires_tunnel":  "remove 需要隧道名称或标识",
	"err.host_option_required":    "必须给出 --host",
	"msg.tunnel_added":            "已添加隧道 %s（%s）",
	"msg.tunnel_updated":          "已更新隧道 %s（%s）",
	"msg.tunnel_removed":          "已删除隧道 %s（%s）",

	// 服务
	"err.service_requires_action": "service 需要 start、stop、restart 或 status",
	"err.unknown_service_action":  "未知的 service 操作 %q",
	"msg.service_started":         "服务已启动（%s）",
	"msg.background_started":      "后台实例已启动（%s）",
	"msg.service_stopped":         "服务已停止（%s）",
	"msg.background_stopped":      "后台实例已停止（%s）",
	"msg.service_state":           "服务 %s（%s）",
	"msg.not_running":             "未运行（%s）",
	"msg.running_pid":             "运行中，pid %d（%s）",

	// 配置校验
	"err.host_address_required": "必须填写主机地址",
	"err.port_out_of_range":     "端口 %d 超出范围",
	"err.ssh_user_required":     "必须填写 SSH 用户",
	"err.password_required":     "密码认证必须填写密码",
	"err.key_required":          "私钥认证必须填写私钥路径或粘贴私钥内容",
	"err.key_path_required":     "私钥认证必须填写私钥路径",
	"err.key_not_readable":      "私钥 %s 不可读：%w",
	"err.unknown_auth_type":     "未知的认证方式 %q",
	"err.tunnel_needs_host":     "隧道必须归属一台主机",
	"err.unknown_tunnel_type":   "未知的隧道类型 %q",
	"err.address_required":      "%s 不能为空",
	"err.address_invalid":       "%s %q 无效：%w",
	"err.address_missing_host":  "%s %q 缺少主机部分",
	"err.address_invalid_port":  "%s %q 的端口无效",
	"err.host_prefix":           "主机 %s：%w",
	"err.tunnel_prefix":         "隧道 %s：%w",
	"err.unknown_host_ref":      "隧道 %s：未知的主机 %s",
	"field.private_key_text":    "私钥内容",
	"field.listen_address":      "监听地址",
	"field.target_address":      "目标地址",

	// 配置文件读写
	"err.read_config":       "读取配置 %s 失败：%w",
	"err.parse_config":      "解析配置 %s 失败：%w",
	"err.create_config_dir": "创建配置目录失败：%w",
	"err.encode_config":     "编码配置失败：%w",
	"err.write_config":      "写入配置失败：%w",
	"err.replace_config":    "替换配置失败：%w",

	// 凭据存储
	"err.open_secret_store": "打开凭据存储失败：%w",
	"err.read_secret":       "读取凭据失败：%w",
	"err.write_secret":      "写入凭据失败：%w",
	"err.read_stdin":        "读取标准输入失败：%w",
	"err.stdin_used":        "标准输入只能读取一次",
	"err.env_var_missing":   "环境变量 %s 不存在",

	// 隧道管理
	"err.manager_closed":         "管理器已关闭",
	"err.tunnel_already_running": "隧道 %q 已在运行",
	"err.tunnel_host_not_found":  "隧道 %q：找不到主机 %q",
	"err.tunnel_invalid":         "隧道 %q 无效：%w",
	"err.host_invalid":           "主机 %q 无效：%w",
	"err.ssh_connection_closed":  "SSH 连接已关闭",
	"detail.stopped":             "已停止",
	"detail.connecting":          "连接中",
	"detail.retry_in":            "将在 %s 后重试",
	"detail.connected_to":        "已连接到 %s",
	"detail.connection_lost":     "连接已断开",
	"log.starting_tunnel":        "正在启动隧道 %s（%s）→ %s",
	"log.stopping_tunnel":        "正在停止隧道 %s",
	"log.cannot_start":           "无法启动 %s：%v",
	"log.connected":              "隧道 %s 已连接到 %s",
	"log.lost":                   "隧道 %s 断开：%v",
	"log.tunnel_error":           "隧道 %s：%v",

	// SSH 传输
	"err.dial":                  "连接 %s 失败：%w",
	"err.handshake":             "与 %s 的 SSH 握手失败：%w",
	"err.ssh_dial":              "SSH 通道连接 %s 失败：%w",
	"err.ssh_listen":            "SSH 远端监听 %s 失败：%w",
	"err.ssh_password_required": "缺少密码",
	"err.ssh_key_required":      "缺少私钥路径或私钥内容",
	"err.ssh_key_path_required": "缺少私钥路径",
	"err.read_key":              "读取私钥 %s 失败：%w",
	"err.parse_key":             "解析私钥 %s 失败：%w",
	"err.no_agent":              "没有可用的 SSH Agent（SSH_AUTH_SOCK）",
	"err.connect_agent":         "连接 SSH Agent %s 失败：%w",
	"err.locate_home":           "定位主目录失败：%w",
	"err.read_known_hosts":      "读取 known_hosts %s 失败：%w",
	"err.parse_known_hosts":     "解析 known_hosts %s 失败：%w",
	"err.append_known_hosts":    "写入 known_hosts 失败：%w",
	"err.write_known_hosts":     "写入 known_hosts 失败：%w",
	"log.keepalive_failed":      "保活失败：%v",

	"log.local_forward_listening":  "本地转发 %s → %s 已开始监听",
	"log.local_forward_failed":     "本地转发 %s → %s：%v",
	"log.remote_forward_listening": "远程转发 %s → %s 已开始监听",
	"log.remote_forward_failed":    "远程转发 %s → %s：%v",
	"log.socks5_listening":         "SOCKS5 代理已在 %s 上监听",
	"log.socks5_handshake_failed":  "SOCKS5 握手失败：%v",
	"log.socks5_request_failed":    "SOCKS5 请求失败：%v",
	"log.socks5_connect_failed":    "SOCKS5 连接 %s 失败：%v",

	"err.cannot_listen_text": "无法监听 %s：%v",
	// 转发
	"err.cannot_listen":             "无法监听 %s：%w",
	"err.unsupported_socks_version": "不支持的 SOCKS 版本 %d",
	"err.unsupported_command":       "不支持的命令 %d",
	"err.unsupported_address_type":  "不支持的地址类型 %d",

	"err.autostart_unsupported":           "当前平台不支持自启动",
	"err.unknown_autostart_kind":          "未知的自启动方式 %q，可用：%v",
	"err.unknown_autostart_kind_short":    "未知的自启动方式 %q",
	"err.no_service_manager":              "%q 没有服务管理器",
	"err.windows_no_service_manager":      "Windows 没有服务管理器，请直接启动 %s",
	"err.windows_no_service_manager_stop": "Windows 没有服务管理器，请从托盘退出 %s",
	"err.instance_already_running":        "已有实例在运行",
	"err.native_window_unavailable":       "当前构建没有原生窗口",
	"err.webview_failed":                  "webview 窗口失败：%v",
	"err.webview_create":                  "无法创建 webview 窗口",
	"err.create_log_dir":                  "创建日志目录失败：%w",
	"err.open_log_file":                   "打开日志文件失败：%w",
	"err.locate_executable":               "定位可执行文件失败：%w",

	"err.create_lock_dir":          "创建锁目录失败：%w",
	"err.open_lock_file":           "打开锁文件 %s 失败：%w",
	"err.lock_file":                "锁定 %s 失败：%w",
	"err.create_mutex":             "创建互斥体 %s 失败：%w",
	"err.open_run_key":             "打开注册表启动项失败：%w",
	"err.read_autostart_value":     "读取自启动项失败：%w",
	"err.delete_autostart_value":   "删除自启动项失败：%w",
	"err.write_autostart_value":    "写入自启动项失败：%w",
	"err.no_service_manager_short": "Windows 没有服务管理器",
	"err.remove_desktop_entry":     "删除桌面自启动项失败：%w",
	"err.create_autostart_dir":     "创建自启动目录失败：%w",
	"err.write_desktop_entry":      "写入桌面自启动项失败：%w",
	"err.create_wants_dir":         "创建 wants 目录失败：%w",
	"err.remove_old_link":          "删除旧软链失败：%w",
	"err.link_unit":                "建立服务软链失败：%w",
	"err.create_unit_dir":          "创建服务目录失败：%w",
	"err.write_unit_file":          "写入服务文件失败：%w",
	"err.write_crontab":            "写入 crontab 失败：%v（%s）",
	"err.no_unit_dir":              "找不到服务目录",
	"err.systemctl":                "systemctl %s 失败：%v（%s）",

	// 后台实例
	"err.instance_running":     "后台实例已在运行",
	"err.start_instance":       "启动后台实例失败：%w",
	"err.instance_exited":      "后台实例立即退出（%v）；可能已有实例在运行",
	"err.instance_not_started": "后台实例没有启动（pid %d）",
	"err.no_instance":          "没有记录的后台实例",
	"err.locate_process":       "定位进程 %d 失败：%w",
	"err.stop_process":         "停止进程 %d 失败：%w",
	"err.process_not_stopped":  "进程 %d 没有停止",
	"err.create_state_dir":     "创建状态目录失败：%w",
	"err.write_pid_file":       "写入 pid 文件失败：%w",

	// 图形界面
	"err.auth_failed":  "认证失败：%w",
	"msg.connected_to": "已成功连接 %s",
	"err.host_missing": "主机不存在",

	"err.tray_unsupported":   "当前平台不支持系统托盘",
	"err.tray_window_create": "无法创建托盘窗口",
	"err.tray_message_loop":  "托盘消息循环失败",

	"autostart.desktop":         "桌面自启动（XDG）",
	"autostart.systemd-user":    "systemd 用户服务",
	"autostart.systemd-system":  "systemd 系统服务",
	"autostart.cron":            "crontab 的 @reboot",
	"autostart.registry":        "Windows 启动项",
	"field.keepalive":           "保活间隔",
	"field.connect_timeout":     "连接超时",
	"field.reconnect_delay":     "重连间隔",
	"field.max_reconnect_delay": "最大重连间隔",
	"err.setting_out_of_range":  "%s必须在 %d 到 %d 之间",
	"err.setting_max_too_small": "%s不能小于%s",
	"log.cannot_restart":        "无法重启隧道 %s：%v",

	// 托盘
	"tray.tip":  "SSH-Tunnel · SSH 隧道与 SOCKS5 代理",
	"tray.open": "打开界面",
	"tray.quit": "退出",

	// 名称
	"type.local":    "本地转发",
	"type.remote":   "远程转发",
	"type.socks5":   "SOCKS5 代理",
	"auth.key":      "私钥认证",
	"auth.password": "密码认证",
	"auth.agent":    "SSH Agent",
}
