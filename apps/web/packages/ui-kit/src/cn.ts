import { clsx, type ClassValue } from 'clsx'
import { twMerge } from 'tailwind-merge'

// cn 合并条件类名并消解 Tailwind 冲突，所有共享组件统一使用。
export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
