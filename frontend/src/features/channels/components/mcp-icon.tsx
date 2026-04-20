import React from 'react';

interface MCPIconProps {
  size?: number | string;
  className?: string;
  style?: React.CSSProperties;
}

export const MCPIcon: React.FC<MCPIconProps> = ({
  size = 20,
  className = '',
  style = {},
  ...rest
}) => {
  return (
    <svg
      fillRule="evenodd"
      height={size}
      style={{ flex: '0 0 auto', lineHeight: 1, ...style }}
      viewBox="0 0 512 512"
      width={size}
      xmlns="http://www.w3.org/2000/svg"
      className={className}
      {...rest}
    >
      <defs>
        <linearGradient id="mcpGradient" x1="0%" y1="0%" x2="100%" y2="100%">
          <stop offset="0%" stopColor="#6366f1" />
          <stop offset="100%" stopColor="#8b5cf6" />
        </linearGradient>
      </defs>
      <path
        fill="url(#mcpGradient)"
        d="M256 48L437.82 184v144L256 464 74.18 328V184L256 48z"
      />
      <circle fill="white" cx="256" cy="176" r="24" />
      <circle fill="white" cx="176" cy="272" r="24" />
      <circle fill="white" cx="336" cy="272" r="24" />
      <path
        stroke="white"
        strokeWidth="8"
        strokeLinecap="round"
        d="M256 200 L256 248 M200 272 L312 272 M200 272 L256 248 M312 272 L256 248"
        fill="none"
      />
      <text
        x="256"
        y="390"
        textAnchor="middle"
        fill="white"
        fontSize="120"
        fontWeight="bold"
        fontFamily="system-ui, sans-serif"
      >
        M
      </text>
    </svg>
  );
};

export default MCPIcon;