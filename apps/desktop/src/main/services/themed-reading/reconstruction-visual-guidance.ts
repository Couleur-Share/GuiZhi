/** 自由 HTML 阅读页的图解策略；不增加模型调用或图片生成开关。 */
export const READING_VISUAL_GUIDANCE = `
按内容需要主动设计内联 SVG 图解：先判断编辑稿中是否有值得可视化的流程、因果、结构、比较或时间关系，再选择图型与所在章节；适合时优先画少量有信息价值的图，不适合时可以完全不用。用户要求纯文字或克制排版时遵循用户偏好。不要把规划过程写到页面，不强制每章一图。
流程用流程图，机制用关系图，概念比较用对照示意，演变用时间轴。图形中的事实、数值、比例和因果关系必须来自编辑稿；没有数据就画定性关系，不能编造数值或用面积大小暗示未经支持的差异。图解放在相关正文旁，正文保留必要解释；装饰插画少量使用，不占满首屏。
SVG 直接写入 html，无需图片模型、不占图片额度；图片生成关闭时仍可按需绘制。只使用 svg/g/path/circle/ellipse/rect/line/polyline/polygon/text/tspan/defs/linearGradient/radialGradient/stop/marker/clipPath/title/desc。支持 transform、渐变、箭头、虚线和裁剪。只允许 url(#已有ID) 引用页内渐变/marker/clipPath，可用于 fill、stroke、marker-start/mid/end、clip-path 属性及 CSS；禁止外部资源、use/image/foreignObject、滤镜、动画和脚本。标签与属性保留 SVG 大小写（viewBox、linearGradient、markerWidth 等），ID 在整页唯一，引用必须存在。
图解使用 viewBox、width:100%、height:auto 与合理纵横比；窄屏约360px下标签仍清晰可读，避免将宽图整体缩到文字过小，必要时用纵向结构或配合HTML图注。text/tspan使用真实文字，保留换行空间，不把文字转成路径。配色使用 CSS 的成对主题变量，文字随深浅主题保持对比度。信息图使用 role="img" 和 aria-label，或 title/desc 搭配 aria-labelledby/aria-describedby；纯装饰图使用 aria-hidden="true"。`;
