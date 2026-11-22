import type { CSSProperties } from "react";
import type { ReaderLayout } from "./protocol";

/**
 * 阅读区的小数像素版面（台账 B-068）→ 页面根节点的内联样式。
 *
 * 渲染层里阅读区的起点与尺寸可以带小数（根字号 18 px 时是 698, 222.75, 582×577.25），Chromium 按小数起点排版并把盒子吸附到设备像素；
 * 岛是真子窗口，Viva 把它的边界按物理像素四舍五入，页面总是从整数像素起点画，小数部分丢了。宿主（viva/reader/layout.go）把
 * 「真实起点减去取整起点」（dx、dy，可为负：222.75 - 223 = -0.25）与精确的逻辑尺寸发来，这里把根节点挪过去并按精确尺寸设宽高：
 * - 用相对定位（left / top）挪，不用 transform：版面偏移之后盒子照常吸附到设备像素，与渲染层是同一条路径；
 *   transform 会让盒子落在小数设备像素上，被抗锯齿成一圈灰边，边线与文字都和渲染层不同。
 * - 相对定位不改变 position: fixed 的包含块，也不影响滚动条与 sticky 元素（它们都在根节点之内）。
 * - 子窗口比内容多出或少出的那不到一个设备像素，由根节点自己的 overflow:hidden 与窗口边界裁掉。
 *
 * 没有版面（null：阅读区落在设备像素上）时返回 undefined，根节点不带任何内联样式——整数坐标是严格的 no-op。
 */
export function layoutStyle(layout: ReaderLayout | null): CSSProperties | undefined {
  if (!layout) return undefined;
  return {
    left: layout.dx,
    top: layout.dy,
    width: layout.width,
    height: layout.height,
  };
}
