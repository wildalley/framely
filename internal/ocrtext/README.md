中文分词使用 jieba 的 MIT 许可词频表，来源提交 `67fa2e36e72f69d9134b8a1037b83fbb070b9775`。保留频率最高的 60,000 个 2–8 字汉语词，gzip 压缩后 338279 字节；补充「截图、选词、贴图、长图、分词」等产品词。采用词频动态规划分词，未知词可切换到单字选择。不包含在线服务或神经网络模型。

`LICENSE.jieba` 保留原始 MIT 许可，组件构建时复制到发行包的 `workers/framely-ocr/licenses`，Arch 同时安装到系统许可证目录。

Source: https://github.com/fxsjy/jieba/blob/67fa2e36e72f69d9134b8a1037b83fbb070b9775/jieba/dict.txt
