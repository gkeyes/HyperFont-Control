# 构建

需要 Go 1.24+、Python 3、Node.js、JDK 17、Android SDK，以及支持 Android target 的 Clang 和 ELF LLD。原生兼容层不依赖 C++ 运行库。标准构建用 SDK platform `android-35`、build-tools `35.0.0`，生成的 DEX 最低 API 为 29；运行时按设备的实际 Android 解析器校验。

```sh
sdkmanager 'platforms;android-35' 'build-tools;35.0.0'
export ANDROID_SDK_ROOT=/absolute/path/to/android-sdk
export JAVA_HOME=/absolute/path/to/jdk-17
export HFC_CLANG=/absolute/path/to/clang++
export HFC_LLD=/absolute/path/to/ld.lld
HFC_BUILD_JOBS=1 python3 tools/build.py
```

请使用本机实际路径。`HFC_ANDROID_PLATFORM`、`HFC_ANDROID_BUILD_TOOLS` 可选择已安装的 SDK；`HFC_DIST` 可改变产物目录。默认 `dist/` 输出 ZIP、SHA-256 与构建报告。`SOURCE.txt` 记录 Git 提交。

Mac 上如果安装了算力管家，按本机规则先查询额度并通过 `perfpilot run build --` 包装构建。GitHub Actions 在 Ubuntu 上执行同一构建脚本。

构建会运行 Go 单元测试和 vet、Web 字重回归、JavaScript/脚本语法检查，编译 arm64/x86_64 核心与 Zygisk 库、Android 校验器，最后检查 ELF 接口、16 KiB 段对齐、ZIP CRC 和无字体载荷。设备诊断测试默认跳过，不能把 CI 通过视作手机开机或视觉验收。

可选 `HFC_REAL_FONTS` 指向包含 `MiSansVF.ttf`、`MiSansLatinVF.ttf`、`xhs-font_normal.ttf` 的自有测试目录，启用历史真实字体的解析和字形数据保留检查。仓库和 Actions 不下载或分发这些文件。

## 发布与在线更新

`module.prop` 的 `updateJson` 指向 `releases/latest/download/update.json`。在 GitHub 以 `v版本号` 标签（如 `v2.2.0`，须与 `module.prop` 一致）发布正式版后，`Release assets` 工作流用该标签的源码重新构建，并把 ZIP、SHA-256、`update.json`、`changelog.md`（取自 CHANGELOG 对应小节）附到这个 Release。Root 管理器随后提示更新。预发布版不算 latest，不会推送；标签与版本号不一致时工作流失败，不上传任何文件。

只更新版本时，同时修改 `module/module.prop`、安装提示、Web 页脚、`internal/engine/model.go` 和 `tools/build.py`。发布必须使用通过检查的同一提交生成的 ZIP。
