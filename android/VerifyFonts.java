package local.hyperfont;

import android.graphics.fonts.Font;
import android.util.Xml;
import java.io.ByteArrayInputStream;
import java.io.File;
import java.io.InputStream;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.nio.file.Files;
import java.util.Map;
import org.json.JSONArray;
import org.json.JSONObject;
import java.util.Iterator;
import org.xmlpull.v1.XmlPullParser;

/** Runs only while preparing a profile, never in the boot mount path. */
public final class VerifyFonts {
    private static File child(File root, String name) throws Exception {
        File file = new File(root, name).getCanonicalFile();
        if (!file.getPath().startsWith(root.getPath() + "/")) {
            throw new IllegalArgumentException("file outside generation");
        }
        return file;
    }

    private static void verifyConfig(File file) throws Exception {
        if (file.length() > 4 * 1024 * 1024) throw new IllegalArgumentException("font XML too large");
        byte[] bytes = Files.readAllBytes(file.toPath());
        XmlPullParser peek = Xml.newPullParser();
        peek.setInput(new ByteArrayInputStream(bytes), null);
        peek.nextTag();
        // Use Android's actual parser, independently of the Go structural guard.
        // The stream overload retains not-yet-mounted filenames for syntax checks;
        // Font.Builder below separately validates the generated font bytes.
        try (InputStream input = new ByteArrayInputStream(bytes)) {
            if ("familyset".equals(peek.getName())) {
                Method parse = Class.forName("android.graphics.FontListParser")
                        .getDeclaredMethod("parse", InputStream.class);
                parse.setAccessible(true);
                parse.invoke(null, input);
            } else if ("fonts-modification".equals(peek.getName())) {
                Method parse = Class.forName("android.graphics.fonts.FontCustomizationParser")
                        .getDeclaredMethod("parse", InputStream.class, String.class, Map.class);
                parse.setAccessible(true);
                parse.invoke(null, input, "/product/fonts/", null);
            } else {
                throw new IllegalArgumentException("unsupported font XML root " + peek.getName());
            }
        }
    }

    public static void main(String[] args) {
        try {
            if (args.length != 1) throw new IllegalArgumentException("generation directory required");
            File root = new File(args[0]).getCanonicalFile();
            JSONObject manifest = new JSONObject(new String(Files.readAllBytes(new File(root, "manifest.json").toPath()), "UTF-8"));
            JSONArray targets = manifest.getJSONArray("targets");
            for (int i = 0; i < targets.length(); i++) {
                verifyConfig(child(root, targets.getJSONObject(i).getString("file")));
            }
            System.out.println("HFC_ANDROID_CONFIG_OK " + targets.length());
            JSONObject fonts = manifest.getJSONObject("fonts");
            int count = 0;
            Iterator<String> keys = fonts.keys();
            while (keys.hasNext()) {
                String name = keys.next();
                if (!name.matches("HFC-[a-f0-9]{64}\\.ttf")) throw new IllegalArgumentException("invalid font path");
                Font font = new Font.Builder(child(root, "fonts/" + name)).setTtcIndex(0).build();
                if (font.getBuffer().remaining() == 0) throw new IllegalStateException("empty font");
                count++;
            }
            System.out.println("HFC_ANDROID_FONT_OK " + count);
        } catch (Throwable e) {
            while (e instanceof InvocationTargetException && e.getCause() != null) e = e.getCause();
            System.err.println("HFC_ANDROID_FONT_FAILED " + e.getClass().getSimpleName() + ": " + e.getMessage());
            System.exit(1);
        }
    }
}
