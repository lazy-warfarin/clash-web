import { createTheme } from '@mui/material/styles'

// clash-verge-rev default dark palette (src/pages/_theme.tsx defaultDarkTheme).
export const darkTheme = createTheme({
  palette: {
    mode: 'dark',
    primary: { main: '#0A84FF' },
    secondary: { main: '#FF9F0A' },
    info: { main: '#0A84FF' },
    error: { main: '#FF453A' },
    warning: { main: '#FF9F0A' },
    success: { main: '#30D158' },
    text: { primary: '#FFFFFF', secondary: 'rgba(235,235,245,.6)' },
    background: { default: '#2E303D', paper: '#2E303D' },
    divider: 'rgba(255,255,255,.06)',
  },
  shape: { borderRadius: 12 },
  typography: {
    fontFamily: `-apple-system, BlinkMacSystemFont, "Microsoft YaHei UI", "Microsoft YaHei", Roboto, "Helvetica Neue", Arial, sans-serif`,
    h4: { fontWeight: 760, letterSpacing: '-.035em' },
    h5: { fontWeight: 720, letterSpacing: '-.025em' },
    h6: { fontWeight: 680 },
    button: { textTransform: 'none', fontWeight: 700 },
  },
  components: {
    MuiCard: { styleOverrides: { root: { backgroundImage: 'none', border: '1px solid rgba(255,255,255,.06)', boxShadow: 'none' } } },
    MuiButton: { defaultProps: { disableElevation: true }, styleOverrides: { root: { borderRadius: 9 } } },
    MuiChip: { styleOverrides: { root: { fontWeight: 700 } } },
  },
})
