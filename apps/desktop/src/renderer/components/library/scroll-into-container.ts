/**
 * 在指定滚动容器内把子元素滚到垂直居中。
 * 不用 element.scrollIntoView：会带动外层 Modal / 页面一起滚。
 */
export function scrollElementIntoContainer(
  container: HTMLElement,
  target: HTMLElement,
  behavior: ScrollBehavior = "smooth",
): void {
  const containerRect = container.getBoundingClientRect();
  const targetRect = target.getBoundingClientRect();
  const delta =
    targetRect.top -
    containerRect.top -
    containerRect.height / 2 +
    targetRect.height / 2;
  container.scrollTo({
    top: container.scrollTop + delta,
    behavior,
  });
}
