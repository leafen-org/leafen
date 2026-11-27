package com.wails.app;

import android.content.Intent;
import android.graphics.Color;
import android.os.Bundle;
import android.view.Gravity;
import android.view.ViewGroup;
import android.widget.FrameLayout;
import android.widget.ImageButton;

import androidx.appcompat.app.AppCompatActivity;
import androidx.camera.core.CameraSelector;
import androidx.camera.core.ImageAnalysis;
import androidx.camera.core.ImageProxy;
import androidx.camera.core.Preview;
import androidx.camera.lifecycle.ProcessCameraProvider;
import androidx.camera.view.PreviewView;
import androidx.core.content.ContextCompat;

import com.google.common.util.concurrent.ListenableFuture;
import com.google.zxing.BarcodeFormat;
import com.google.zxing.BinaryBitmap;
import com.google.zxing.DecodeHintType;
import com.google.zxing.PlanarYUVLuminanceSource;
import com.google.zxing.ReaderException;
import com.google.zxing.Result;
import com.google.zxing.common.HybridBinarizer;
import com.google.zxing.qrcode.QRCodeReader;

import java.nio.ByteBuffer;
import java.util.Collections;
import java.util.EnumMap;
import java.util.Map;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.atomic.AtomicBoolean;

/**
 * The camera full screen until it reads a QR code (LeafenCodeScanner.java):
 * CameraX shows it and hands each frame's brightness to ZXing. The close
 * button or Back ends it with nothing.
 */
public class LeafenCodeScanActivity extends AppCompatActivity {
    static final String EXTRA_TEXT = "text";
    static final int RESULT_UNAVAILABLE = RESULT_FIRST_USER;

    private final AtomicBoolean answered = new AtomicBoolean(false);
    private final QRCodeReader reader = new QRCodeReader();
    private final Map<DecodeHintType, Object> hints = new EnumMap<>(DecodeHintType.class);
    private ExecutorService frames;

    @Override
    protected void onCreate(Bundle state) {
        super.onCreate(state);
        hints.put(DecodeHintType.POSSIBLE_FORMATS, Collections.singletonList(BarcodeFormat.QR_CODE));
        hints.put(DecodeHintType.TRY_HARDER, Boolean.TRUE);
        FrameLayout root = new FrameLayout(this);
        root.setBackgroundColor(Color.BLACK);
        PreviewView preview = new PreviewView(this);
        root.addView(preview, new FrameLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, ViewGroup.LayoutParams.MATCH_PARENT));
        ImageButton close = new ImageButton(this);
        close.setImageResource(android.R.drawable.ic_menu_close_clear_cancel);
        close.setBackgroundColor(Color.TRANSPARENT);
        close.setContentDescription(getString(android.R.string.cancel));
        close.setOnClickListener(view -> finish());
        int margin = Math.round(16 * getResources().getDisplayMetrics().density);
        FrameLayout.LayoutParams corner = new FrameLayout.LayoutParams(
                ViewGroup.LayoutParams.WRAP_CONTENT, ViewGroup.LayoutParams.WRAP_CONTENT, Gravity.TOP | Gravity.START);
        corner.setMargins(margin, margin * 2, margin, margin);
        root.addView(close, corner);
        setContentView(root);

        frames = Executors.newSingleThreadExecutor();
        ListenableFuture<ProcessCameraProvider> cameras = ProcessCameraProvider.getInstance(this);
        cameras.addListener(() -> {
            try {
                ProcessCameraProvider provider = cameras.get();
                Preview shown = new Preview.Builder().build();
                shown.setSurfaceProvider(preview.getSurfaceProvider());
                ImageAnalysis analysis = new ImageAnalysis.Builder()
                        .setBackpressureStrategy(ImageAnalysis.STRATEGY_KEEP_ONLY_LATEST)
                        .build();
                analysis.setAnalyzer(frames, this::read);
                CameraSelector camera = provider.hasCamera(CameraSelector.DEFAULT_BACK_CAMERA)
                        ? CameraSelector.DEFAULT_BACK_CAMERA : CameraSelector.DEFAULT_FRONT_CAMERA;
                provider.unbindAll();
                provider.bindToLifecycle(this, camera, shown, analysis);
            } catch (Exception error) {
                setResult(RESULT_UNAVAILABLE);
                finish();
            }
        }, ContextCompat.getMainExecutor(this));
    }

    // The frame's brightness plane is all a QR code needs, in any rotation.
    private void read(ImageProxy image) {
        try {
            ImageProxy.PlaneProxy brightness = image.getPlanes()[0];
            ByteBuffer buffer = brightness.getBuffer();
            byte[] data = new byte[buffer.remaining()];
            buffer.get(data);
            PlanarYUVLuminanceSource source = new PlanarYUVLuminanceSource(data, brightness.getRowStride(),
                    image.getHeight(), 0, 0, image.getWidth(), image.getHeight(), false);
            Result result = reader.decode(new BinaryBitmap(new HybridBinarizer(source)), hints);
            if (result.getText() != null && !result.getText().isEmpty() && answered.compareAndSet(false, true)) {
                Intent scanned = new Intent().putExtra(EXTRA_TEXT, result.getText());
                runOnUiThread(() -> {
                    setResult(RESULT_OK, scanned);
                    finish();
                });
            }
        } catch (ReaderException | IllegalArgumentException | ArrayIndexOutOfBoundsException notFound) {
            // No code in this frame.
        } finally {
            reader.reset();
            image.close();
        }
    }

    @Override
    protected void onDestroy() {
        super.onDestroy();
        if (frames != null) frames.shutdown();
    }
}
