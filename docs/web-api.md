# Web API

固定入口：`/data/adb/modules/hyperfont_control/bin/hfctl request BASE64_JSON`。响应为 `{ok:true,data:...}` 或 `{ok:false,error:"..."}`。未定义字段和操作拒绝。最大请求长度 120000 个 Base64 字符。

| 操作 | 输入 | 结果 |
|---|---|---|
| status | 无 | 字体库、草稿、active/pending、当前启动记录与文件可见性 |
| init | 无 | 首次读取 ROM 原生字体表 |
| import_path | path, 可选 label | 仅从 Download 导入；返回内容哈希和元数据 |
| upload_begin | label, size | 随机 token |
| upload_chunk | token, offset, chunk | Base64 字节，最大 48 KiB；严格检查连续偏移 |
| upload_finish | token | 解析并登记字体；未完成文件不进入字体库 |
| upload_cancel | token | 只清理本次导入 |
| cleanup_uploads | 无 | 清理所有有效 token 的未完成上传，不删除字体库 |
| validate | profile | 完整语义校验与提示 |
| save | profile | 保存草稿，不切换字体 |
| stage | profile | 生成、校验、提交 pending，需手动重启 |
| restore | 无 | 提交无覆盖配置，需手动重启 |
| cancel_pending | 无 | 撤销未生效提交 |
| delete_font | id | 拒绝删除被 active/pending/draft 引用的字体 |
| export_profile | profile | 在 Download 导出配置 JSON |
| diagnostics / export_diagnostics | 无 | 诊断对象 / 在 Download 保存诊断 JSON |
| refresh_baseline | 无 | 未处于覆盖状态时重新识别 ROM |

`boot` 和 `boot-complete` 是模块生命周期的独立 CLI 命令，不通过 Web API 提供。仅开机阶段运行，不允许用 WebUI 热挂载或自动重启。

状态分层：

- draft：已保存用户设置。
- pending：已生成并通过校验，未代表当前字体已变。
- active：最近一次挂载成功选中的版本，不等同于 Android 完成开机。
- boot.state=mounted：本次启动挂载完成。
- boot.state=guarded：上次开机未确认，同一配置已暂停。
- visibilityError：内容校验失败原因，供诊断与面板显示。
- visible=true：当前命名空间中的配置与字体文件内容哈希匹配。
- 界面视觉、每个控件的字形与粗细：仍需实际应用验收。

配置契约位于 `config/profile.schema.json`，资源存在性、字体真实轴范围、重复权重和单调映射由后端再校验。示例 JSON 的全零字体 ID 是占位值，不能直接提交。
