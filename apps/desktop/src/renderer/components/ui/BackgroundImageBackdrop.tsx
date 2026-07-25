import { LocalImage } from "./LocalImage";

interface BackgroundImageBackdropProps {
  src: string;
  opacity: number;
  blur: number;
  alt: string;
}

export function BackgroundImageBackdrop({
  src,
  opacity,
  blur,
  alt,
}: BackgroundImageBackdropProps) {
  return (
    <>
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 z-0 overflow-hidden"
        style={{
          opacity,
          filter: `blur(${blur}px)`,
          transform: blur > 0 ? "scale(1.03)" : undefined,
        }}
      >
        <LocalImage
          src={src}
          alt={alt}
          className="h-full w-full object-cover object-center"
          fallbackClassName="h-full w-full"
        />
      </div>
      <div
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 z-0 app-wallpaper-blanket"
      />
    </>
  );
}
