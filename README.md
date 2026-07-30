# gong（锣）

I'm outta here!

## 安装

```bash
brew install xwvike/tap/gong
gong on
```

默认启用两条定时：#1 12:00、#2 18:00，周一到周五。


```bash
gong set            # 进入定时器管理
gong ls             # 列出现有定时
gong themes         # 列出可用主题
gong vis <theme>    # 预览一个主题
gong stop           # 掐掉正在播的浮层
gong off            # 关掉 gong，但保留配置和程序
```

## 主题

### default

<img src="docs/default.png" alt="default" width="720">

### tunnel

<img src="docs/tunnel.gif" alt="tunnel" width="720">

## 卸载gong

```bash
gong uninstall
```

参数 `--purge` 会同时清除自定义主题目录 `~/.config/gong`。

**别直接 `brew uninstall gong`** —— formula 没有 uninstall hook，plist 会留在
`~/Library/LaunchAgents`，之后每天到点去拉一个不存在的二进制，而且是静默失败。
`gong uninstall` 会先清 plist、从 launchd 撤出，最后自动帮你跑 `brew uninstall`。
