import React, { useEffect, useRef } from 'react';

interface CheckboxProps {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label?: string;
  ariaLabel?: string;
  className?: string;
  disabled?: boolean;
  indeterminate?: boolean;
}

export function Checkbox({
  checked,
  onChange,
  label,
  ariaLabel,
  className = '',
  disabled = false,
  indeterminate = false,
}: CheckboxProps) {
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    if (inputRef.current) {
      inputRef.current.indeterminate = indeterminate;
    }
  }, [indeterminate]);

  const active = checked || indeterminate;

  return (
    <label 
      className={`flex items-center gap-2 cursor-pointer select-none ${disabled ? 'opacity-50 cursor-not-allowed' : ''} ${className}`}
      onClick={(e) => {
        if (disabled) return;
        e.preventDefault();
        onChange(!checked);
      }}
    >
      <div className="relative flex items-center">
        <input
          ref={inputRef}
          type="checkbox"
          className="peer sr-only"
          checked={checked}
          readOnly
          disabled={disabled}
          aria-label={label ? undefined : ariaLabel}
          aria-checked={indeterminate ? 'mixed' : checked}
        />
        <span
          className={`
            inline-flex h-4 w-4 items-center justify-center rounded border border-border bg-background
            transition-colors duration-base
            peer-focus-visible:outline peer-focus-visible:outline-2 peer-focus-visible:outline-primary/50
            ${active ? 'bg-primary border-primary' : 'hover:border-primary/50'}
          `}
          aria-hidden="true"
        >
          <svg
            className={`h-3 w-3 text-primary-foreground transition-opacity duration-base ${active ? 'opacity-100' : 'opacity-0'}`}
            viewBox="0 0 12 12"
            fill="none"
          >
            <path
              d={indeterminate ? 'M2 6H10' : 'M2 6L5 9L10 3'}
              stroke="currentColor"
              strokeWidth="2"
              strokeLinecap="round"
              strokeLinejoin="round"
            />
          </svg>
        </span>
      </div>
      {label && <span className="text-sm text-foreground">{label}</span>}
    </label>
  );
}

