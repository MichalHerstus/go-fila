/** @type {import('tailwindcss').Config} */
module.exports = {
  darkMode: 'class',
  content: ["./internal/views/**/*.templ", "./internal/panel/auth/**/*.templ"],
  theme: {
    extend: {
      colors: {
        brand: {
          // rgb(<channels> / <alpha-value>) — the channel triplet comes from
          // the --brand-*-rgb CSS variables emitted by the Base layout and the
          // login page (viewmodels.BrandChannels / hexChannels). Defined this
          // way so opacity modifiers like bg-brand-primary/10 resolve at
          // runtime; a bare var(--brand-*) would silently drop every /alpha
          // utility from the compiled CSS.
          primary: 'rgb(var(--brand-primary-rgb) / <alpha-value>)',
          secondary: 'rgb(var(--brand-secondary-rgb) / <alpha-value>)',
        },
      },
    },
  },
  safelist: [
    // card view + stats_grid column counts are baked into the emitted class
    // name as an integer (lg:grid-cols-N); one stylesheet must cover any N.
    { pattern: /grid-cols-(1|2|3|4|5|6|7|8|9|10|11|12)/, variants: ['sm', 'md', 'lg'] },
    // max_content_width -> max-w-{V} is interpolated at runtime by the Base
    // layout (theme.MaxContentWidth), never a literal class in the templates.
    'max-w-none', 'max-w-xs', 'max-w-sm', 'max-w-md', 'max-w-lg', 'max-w-xl',
    'max-w-2xl', 'max-w-3xl', 'max-w-4xl', 'max-w-5xl', 'max-w-6xl', 'max-w-7xl',
    'max-w-full', 'max-w-min', 'max-w-max', 'max-w-fit', 'max-w-prose',
    'max-w-screen-sm', 'max-w-screen-md', 'max-w-screen-lg', 'max-w-screen-xl', 'max-w-screen-2xl',
    // renderBadge builds its classes via fmt.Sprintf at runtime (always gray),
    // so the gray badge palette never appears literally in any template.
    'bg-gray-100', 'text-gray-800', 'dark:bg-gray-900/50', 'dark:text-gray-300',
  ],
  plugins: [],
}
