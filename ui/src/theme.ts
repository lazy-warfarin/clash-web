import { createTheme } from '@mui/material/styles'

export const darkTheme = createTheme({
  palette: {
    mode: 'dark',
    primary: { main: '#53c8f2' },
    secondary: { main: '#8d7aff' },
    warning: { main: '#f2a64a' },
    success: { main: '#63d8a1' },
    background: { default: '#0d1220', paper: '#171e30' },
    divider: 'rgba(154, 175, 211, .13)',
  },
  shape: { borderRadius: 12 },
  typography: {
    fontFamily: 'Inter Variable, Noto Sans SC, Segoe UI, sans-serif',
    h4: { fontWeight: 760, letterSpacing: '-.035em' },
    h5: { fontWeight: 720, letterSpacing: '-.025em' },
    h6: { fontWeight: 680 },
    button: { textTransform: 'none', fontWeight: 700 },
  },
  components: {
    MuiCard: { styleOverrides: { root: { backgroundImage: 'none', border: '1px solid rgba(154,175,211,.11)', boxShadow: '0 18px 40px rgba(3,8,18,.16)' } } },
    MuiButton: { defaultProps: { disableElevation: true }, styleOverrides: { root: { borderRadius: 9 } } },
    MuiChip: { styleOverrides: { root: { fontWeight: 700 } } },
  },
})
