using System.Text;
using System.Windows.Media;
using System.Windows.Media.Imaging;
using Docnet.Core;
using Docnet.Core.Models;
using Tesseract;

namespace BuctCourseLocal;

internal record ScheduleTextBox(string Text, double X, double Y, double Width, double Height)
{
    internal double CenterX => X + Width / 2;
    internal double CenterY => Y + Height / 2;
}
internal record ScheduleTextPage(int Width, IReadOnlyList<ScheduleTextBox> Boxes);
internal record ScheduleDocument(string Text, IReadOnlyList<ScheduleTextPage> Pages);

internal static class ScheduleDocumentReader
{
    internal static ScheduleDocument Read(string path)
    {
        if (!Path.GetExtension(path).Equals(".pdf", StringComparison.OrdinalIgnoreCase))
        {
            using var engine = CreateEngine();
            using var image = Pix.LoadFromMemory(File.ReadAllBytes(path));
            var recognized = Recognize(engine, image);
            return new ScheduleDocument(recognized.Text, [recognized.Page]);
        }
        using var pdf = DocLib.Instance.GetDocReader(File.ReadAllBytes(path), new PageDimensions(1600, 2200));
        if (pdf.GetPageCount() > 20)
            throw new FormatException("PDF 最多可导入 20 页。");
        TesseractEngine? ocr = null;
        var text = new StringBuilder();
        var pages = new List<ScheduleTextPage>();
        try
        {
            for (var i = 0; i < pdf.GetPageCount(); i++)
            {
                using var page = pdf.GetPageReader(i);
                var extracted = page.GetText();
                if (string.IsNullOrWhiteSpace(extracted))
                {
                    ocr ??= CreateEngine();
                    using var image = Raster(page.GetImage(), page.GetPageWidth(), page.GetPageHeight());
                    var recognized = Recognize(ocr, image);
                    extracted = recognized.Text;
                    pages.Add(recognized.Page);
                }
                else
                    pages.Add(new ScheduleTextPage(page.GetPageWidth(), page.GetCharacters().Where(c => !char.IsWhiteSpace(c.Char))
                        .Take(40000).Select(c => new ScheduleTextBox(c.Char.ToString(), Math.Min(c.Box.Left, c.Box.Right),
                            Math.Min(c.Box.Top, c.Box.Bottom), Math.Abs(c.Box.Right - c.Box.Left), Math.Abs(c.Box.Bottom - c.Box.Top)))
                        .Where(b => b.Width > 0 && b.Height > 0).ToList()));
                text.AppendLine(extracted);
            }
            return new ScheduleDocument(text.ToString(), pages);
        }
        finally { ocr?.Dispose(); }
    }

    private static Pix Raster(byte[] pixels, int width, int height)
    {
        var bitmap = BitmapSource.Create(width, height, 96, 96, PixelFormats.Bgra32, null, pixels, width * 4);
        var encoder = new PngBitmapEncoder();
        encoder.Frames.Add(BitmapFrame.Create(bitmap));
        using var stream = new MemoryStream();
        encoder.Save(stream);
        return Pix.LoadFromMemory(stream.ToArray());
    }

    private static TesseractEngine CreateEngine() => new(Path.Combine(AppContext.BaseDirectory, "Assets", "Ocr"), "chi_sim+eng", EngineMode.LstmOnly);

    private static (string Text, ScheduleTextPage Page) Recognize(TesseractEngine engine, Pix image)
    {
        if ((long)image.Width * image.Height > 40_000_000)
            throw new FormatException("图片尺寸过大，请缩小后重新导入。");
        using var page = engine.Process(image, PageSegMode.Auto);
        using var iterator = page.GetIterator();
        var boxes = new List<ScheduleTextBox>();
        iterator.Begin();
        do
        {
            var word = iterator.GetText(PageIteratorLevel.Word);
            if (!string.IsNullOrWhiteSpace(word) && iterator.TryGetBoundingBox(PageIteratorLevel.Word, out var rect))
                boxes.Add(new ScheduleTextBox(word, rect.X1, rect.Y1, rect.Width, rect.Height));
        } while (boxes.Count < 40000 && iterator.Next(PageIteratorLevel.Word));
        return (page.GetText(), new ScheduleTextPage(image.Width, boxes));
    }
}
