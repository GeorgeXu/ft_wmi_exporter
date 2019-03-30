# `ft_wmi_exporter` 说明

## 简介

`ft_wmi_exporter` 是驻云基于开源的 Prometheus [wmi_exporter](https://github.com/martinlindhe/wmi_exporter) 项目扩展，用于支持对 Windows 服务器进行数据收集并上报到 `王教授` 的分析诊断平台进行分析的 exporter 项目。

## 支持操作系统

| 系统 | 支持的版本 |
| ----- | -----   |
| Windows Server | >= 2008 |

## 安装

下载 `ft_wmi_exporter` 安装包：

最新版下载地址：[http://cloudcare-files.oss-cn-hangzhou.aliyuncs.com/ft_wmi_exporter/release/ft_wmi_exporter.exe](http://cloudcare-files.oss-cn-hangzhou.aliyuncs.com/ft_wmi_exporter/release/ft_wmi_exporter.exe)

下载成功后，上传到需要监控的 Windows 服务器中，点击 ft_wmi_exporter.exe，设置 监听端口，点击 「安装」，等待安装成功，则 `ft_wmi_exporter` 部署成功

## 配置 Forethought 

部署好 `ft_wmi_exporter`后，在 Forethought 的 `Prometheus` 的配置文件中需要配置抓取的 `target`。

**配置抓取时序数据**

- metrics_path：默认 `/metrics`
- targets：即安装 `ft_wmi_exporter` 服务器的IP地址和监听的端口，需要保证Forethought 的服务器可访问该地址
- scrape_interval：抓取的频率，推荐设置为 `1m` ，及 1 分钟抓取 1 次
- labels：如果没有配置 `uploader_uid` 和 `group_name`，抓取的数据默认不会上传到 `王教授` 的分析诊断平台，`uploader_uid` 和 `group_name` 的获取和设置方法 参见 Forethought 的使用文档
 
示例：

    - job_name: 'metrics_job'
      scrape_interval: 1m
      metrics_path: /metrics
      static_configs:
      - targets: ['172.16.0.85:9100']
      labels:
        uploader_uid: 'uid-3fb91f37-59af-4d3b-bf66-44426fc4afb3'
        group_name: 'demogroup'



**配置抓取kv数据**

- metrics_path：默认 `/kvs/json`
- targets：即安装 `ft_wmi_exporter` 服务器的IP地址和监听的端口，需要保证Forethought 的服务器可访问该地址
- scrape_interval：抓取的频率，推荐设置为 `15m` ，及 15 分钟抓取 1 次
- labels：如果没有配置 `uploader_uid` 和 `group_name`，抓取的数据默认不会上传到 `王教授` 的分析诊断平台，`uploader_uid` 和 `group_name` 的获取和设置方法 参见 Forethought 的使用文档

示例：

    - job_name: 'kvs_job'
    	scrape_interval: 15m
    	metrics_path: /kvs/json
    	static_configs:
    	- targets: ['172.16.0.85:9100']
      	labels:
         uploader_uid: 'uid-3fb91f37-59af-4d3b-bf66-44426fc4afb3'
         group_name: 'demogroup'
         
**注意**

同一个 `ft_wmi_exporter` 抓取 `target` 的 `uploader_uid` 和 `group_name` 必须相同
