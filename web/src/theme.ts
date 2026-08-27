import { createTheme, type MantineColorsTuple } from '@mantine/core';

const atlasBlue: MantineColorsTuple = [
  '#edf8fa', '#d8edf3', '#b3dce8', '#88c9dc', '#61b8d1',
  '#43abc8', '#097b99', '#006f8d', '#005f78', '#004e64',
];

export const atlasTheme = createTheme({
  primaryColor: 'atlasBlue',
  colors: { atlasBlue },
  fontFamily: 'Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif',
  headings: {
    fontFamily: 'Fraunces, Iowan Old Style, Palatino Linotype, Book Antiqua, Georgia, serif',
    fontWeight: '600',
  },
  defaultRadius: 'md',
});
